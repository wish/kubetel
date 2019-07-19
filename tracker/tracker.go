package tracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"syscall"
	"time"

	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	v1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	kcdutil "github.com/wish/kubetel/gok8s/kcdutil"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

//Tracker holds comonents needed to track a deployment
type Tracker struct {
	k8scInformer cache.SharedIndexInformer
	kcdcInformer cache.SharedIndexInformer

	k8scSynced cache.InformerSynced
	kcdcSynced cache.InformerSynced

	queue workqueue.RateLimitingInterface

	deploymentClient appsclientv1.DeploymentInterface

	httpClient *http.Client
	rand       *rand.Rand

	sqsClient *sqs.SQS

	clusterName             string
	version                 string
	deployStatusEndpointAPI string
	kcdStates               map[string]string
}

//NewTracker Creates a new tracker
func NewTracker(k8sClient kubernetes.Interface, customIF informer.SharedInformerFactory, k8sIF k8sinformers.SharedInformerFactory) (*Tracker, error) {
	kcdInformer := customIF.Custom().V1().KCDs()
	k8sInformer := k8sIF.Apps().V1().Deployments()
	deploymentClient := k8sClient.AppsV1().Deployments(viper.GetString("tracker.namespace"))

	var httpClient *http.Client
	var rander *rand.Rand
	var sqsClient *sqs.SQS
	switch endtype := viper.GetString("tracker.endpointtype"); endtype {
	case "http":
		if viper.GetString("tracker.endpoint") != "" {
			httpClient = &http.Client{
				Timeout: time.Duration(5 * time.Second),
			}
			rander = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
	case "sqs":
		sess := session.Must(session.NewSession(&aws.Config{
			Region: aws.String(viper.GetString("region")),
		}))
		sqsClient = sqs.New(sess)
	}

	t := &Tracker{
		k8scInformer: k8sInformer.Informer(),
		kcdcInformer: kcdInformer.Informer(),

		deploymentClient: deploymentClient,

		k8scSynced: k8sInformer.Informer().HasSynced,
		kcdcSynced: kcdInformer.Informer().HasSynced,

		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployTracker"),

		clusterName:             viper.GetString("cluster"),
		version:                 viper.GetString("tracker.version"),
		deployStatusEndpointAPI: viper.GetString("tracker.endpoint"),

		httpClient: httpClient,
		rand:       rander,
		sqsClient:  sqsClient,
		kcdStates:  make(map[string]string),
	}

	t.k8scInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: t.trackDeployment,
	})
	t.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddKcd,
		UpdateFunc: t.trackKcd,
	})
	return t, nil
}

func (t *Tracker) trackDeployment(oldObj interface{}, newObj interface{}) {
	newDeploy, ok := newObj.(*appsv1.Deployment)
	if !ok {
		log.Errorf("Not a deploy object")
		return
	}
	oldDeploy, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		return
	}
	name, ok := oldDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	if name == viper.GetString("tracker.kcdapp") {
		log.Infof("Deployment updated: %s", name)
		t.enqueue(newDeploy)
	}

}

func (t *Tracker) trackKcd(oldObj interface{}, newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	if newKCD.Status.CurrStatus == t.kcdStates[newKCD.Name] {
		return
	}
	podStatus := newKCD.Status.CurrStatus
	log.Infof("KCD status updated: %s", podStatus)
	t.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if podStatus == kcdutil.StatusSuccess || podStatus == kcdutil.StatusFailed {
		log.Trace("Enqueue KCD")
		t.enqueue(newKCD)
	}
}

func (t *Tracker) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	podStatus := newKCD.Status.CurrStatus
	log.Infof("new KCD with status: %s", podStatus)
	t.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if podStatus == kcdutil.StatusSuccess || podStatus == kcdutil.StatusFailed {
		t.enqueue(newKCD)
	}
}

//Adds a new job request into the work queue
func (t *Tracker) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error obtaining key for object being enqueue: %s", err.Error()))
		log.Errorf("Failed to obtain key for object being enqueue: %v", err)
		return
	}
	t.queue.AddRateLimited(key)
}

// Run starts the tracker
func (t *Tracker) Run(threadCount int, stopCh <-chan struct{}, waitgroup *sync.WaitGroup) error {
	defer runtime.HandleCrash()
	defer t.queue.ShutDown()

	log.Info("Starting Tracker")

	go t.kcdcInformer.Run(stopCh)
	go t.k8scInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, t.kcdcSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}
	if !cache.WaitForCacheSync(stopCh, t.k8scSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}

	log.Info("Cache sync completed")

	for i := 0; i < threadCount; i++ {
		waitgroup.Add(1)
		go func() {
			defer waitgroup.Done()
			t.runWorker()
		}()
	}
	log.Info("Started Tracker")

	go func() {
		waitgroup.Wait()
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	}()

	<-stopCh
	t.queue.ShutDown()
	waitgroup.Wait()
	log.Info("Shutting down tracker")
	return nil
}

func (t *Tracker) runWorker() {
	log.Info("Running worker")
	for t.processNextItem() {
	}
}

//dequeues workqueue with retry if failed
func (t *Tracker) processNextItem() bool {
	key, shutdown := t.queue.Get()
	if shutdown {
		return false
	}
	defer t.queue.Done(key)

	err := t.processItem(key.(string))

	maxRetries := viper.GetInt("tracker.maxretries")

	if err == nil {
		t.queue.Forget(key)
	} else if t.queue.NumRequeues(key) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", key, err)
		t.queue.AddRateLimited(key)
	} else {
		log.Errorf("Error processing %s (giving up): %v", key, err)
		t.queue.Forget(key)
		runtime.HandleError(err)
	}

	return true
}

func (t *Tracker) processItem(key string) error {
	obj, exists, err := t.kcdcInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		obj, exists, err = t.k8scInformer.GetIndexer().GetByKey(key)
		if !exists {
			return errors.New("Object does not exists")
		}
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return err
		}
		statusData := StatusData{
			t.clusterName,
			time.Now().UTC(),
			*deployment,
			t.version,
		}
		t.sendDeploymentEvent(t.deployStatusEndpointAPI, statusData)

	} else {
		kcd, ok := obj.(*v1.KCD)
		if !ok {
			return errors.New("Not kcd object")
		}
		t.sendDeployedFinishedEvent(kcd)
		t.queue.ShutDown()

	}
	return nil
}

func (t *Tracker) sendDeployedFinishedEvent(kcd *customv1.KCD) {
	status := kcd.Status.CurrStatus
	success := false
	if status == kcdutil.StatusSuccess {
		success = true
	} else if status == kcdutil.StatusProgressing {
		return
	}
	set := labels.Set(kcd.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}
	deployments, err := t.deploymentClient.List(listOpts)
	if err != nil {
		return
	}
	for _, item := range deployments.Items {
		log.Tracef("Sending: deploy finished")
		deployment := item
		statusData := FinishedStatusData{
			t.clusterName,
			time.Now().UTC(),
			deployment,
			t.version,
			success,
		}
		switch endtype := viper.GetString("tracker.endpointtype"); endtype {
		case "http":
			t.sendDeploymentEventHTTP(fmt.Sprintf("%s/finished", t.deployStatusEndpointAPI), statusData)
		case "sqs":
			t.sendDeploymentEventSQS(t.deployStatusEndpointAPI, "deployFinished", statusData)
		}

	}
}

func (t *Tracker) sendDeploymentEvent(endpoint string, data interface{}) {
	switch endtype := viper.GetString("tracker.endpointtype"); endtype {
	case "http":
		t.sendDeploymentEventHTTP(endpoint, data)
	case "sqs":
		t.sendDeploymentEventSQS(endpoint, "deployStatus", data)
	}

}

func (t *Tracker) sendDeploymentEventHTTP(endpoint string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Failed marshalling the deployment object: %v", err)
		return
	}

	for retries := 0; retries < 5; retries++ {
		t.sleepFor(retries)

		resp, err := t.httpClient.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			log.Errorf("failed to send deployment event to %s: %v", endpoint, err)
			continue
		}
		if resp.StatusCode >= 400 {
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			log.Infof("response: %v", result)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		break
	}
}

func (t *Tracker) sendDeploymentEventSQS(endpoint, messageType string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Failed marshalling the deployment object: %v", err)
		return
	}
	log.Tracef("Sending: deploy message")
	_, err = t.sqsClient.SendMessage(&sqs.SendMessageInput{
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"Type": &sqs.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String(messageType),
			},
			"Version": &sqs.MessageAttributeValue{
				DataType:    aws.String("String"),
				StringValue: aws.String("v1alpha1"),
			},
		},
		MessageBody: aws.String(string(jsonData[:])),
		QueueUrl:    aws.String(t.deployStatusEndpointAPI),
	})
	if err != nil {
		log.Errorf("Failed sending the deployment object: %v", err)
		return
	}
}

func (t *Tracker) sleepFor(attempts int) {
	if attempts < 1 {
		return
	}
	sleepTime := (t.rand.Float64() + 1) + math.Pow(2, float64(attempts-0))
	durationStr := fmt.Sprintf("%ss", strconv.FormatFloat(sleepTime, 'f', 2, 64))
	sleepDuration, _ := time.ParseDuration(durationStr)
	time.Sleep(sleepDuration)
}
