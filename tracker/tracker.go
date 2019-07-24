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
	"sync/atomic"
	"syscall"
	"time"

	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
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

	queue     chan DeployMessage
	queueDone int32

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

		queue: make(chan DeployMessage, 200),

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

//This function can be eventully improved to provide more useful information
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
		deployMessage := DeployMessage{
			Type:    "deployStatus",
			Version: "v1alpha1",
			Body: StatusData{
				t.clusterName,
				time.Now().UTC(),
				*newDeploy,
				t.version,
			},
		}
		t.enqueue(deployMessage)
	}

}

func (t *Tracker) trackKcd(oldObj interface{}, newObj interface{}) {
	t.trackAddKcd(newObj)
}

func (t *Tracker) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	podStatus := newKCD.Status.CurrStatus
	log.Tracef("KCD with status: %s", podStatus)
	t.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if podStatus == kcdutil.StatusSuccess || podStatus == kcdutil.StatusFailed {
		messages := t.deployFinishHandler(newKCD)
		for _, m := range messages {
			t.enqueue(m)
		}
		atomic.StoreInt32(&t.queueDone, 1)
		close(t.queue)
	}
}

func (t *Tracker) deployFinishHandler(kcd *customv1.KCD) (messages []DeployMessage) {
	status := kcd.Status.CurrStatus
	success := false
	if status == kcdutil.StatusSuccess {
		success = true
	}
	set := labels.Set(kcd.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: set.AsSelector().String()}
	deployments, err := t.deploymentClient.List(listOpts)
	if err != nil {
		return
	}
	for _, item := range deployments.Items {
		deployment := item
		deployMessage := DeployMessage{
			Type:    "deployFinished",
			Version: "v1alpha1",
			Body: FinishedStatusData{
				t.clusterName,
				time.Now().UTC(),
				deployment,
				t.version,
				success,
			},
		}
		messages = append(messages, deployMessage)
	}

	if !success {
		failureMessages := t.deployFailureHandler(kcd)
		if failureMessages != nil {
			messages = append(messages, failureMessages...)
		}
	}
	return messages
}

//Placeholder function to grab additional information uppon failure
func (t *Tracker) deployFailureHandler(kcd *customv1.KCD) (messages []DeployMessage) {
	//TODO
	return nil
}

//Adds a new job request into the work queue without
func (t *Tracker) enqueue(m DeployMessage) {
	for !t.attemptEnqeue(m) {
		time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
	}
}

func (t *Tracker) attemptEnqeue(data DeployMessage) bool {
	if atomic.LoadInt32(&t.queueDone) == int32(0) {
		select {
		case t.queue <- data:
			return true
		default:
			return false
		}
	}
	return true
}

// Run starts the tracker
func (t *Tracker) Run(threadCount int, stopCh <-chan struct{}, waitgroup *sync.WaitGroup) error {
	defer runtime.HandleCrash()
	//defer t.queue.ShutDown()

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
	//t.queue.ShutDown()
	waitgroup.Wait()
	log.Info("Shutting down tracker")
	return nil
}

func (t *Tracker) runWorker() {
	log.Info("Running worker")
	for m := range t.queue {
		t.processNextItem(m)
	}
}

func (t *Tracker) processNextItem(data DeployMessage) (success bool) {
	switch endtype := viper.GetString("tracker.endpointtype"); endtype {
	case "http":
		switch messageType := data.Type; messageType {
		case "deployFinish":
			success = t.sendDeploymentEventHTTP(fmt.Sprintf("%s/finished", t.deployStatusEndpointAPI), data)
		case "deployStatus":
			success = t.sendDeploymentEventHTTP(t.deployStatusEndpointAPI, data)
		default:
			log.WithFields(log.Fields{"message_type": messageType}).Warn("Unknown message type for http endpoint")
			success = true //Prevent Retry for bad message
		}
	case "sqs":
		success = t.sendDeploymentEventSQS(t.deployStatusEndpointAPI, data)
	default:
		log.WithFields(log.Fields{"endpoint_type": endtype}).Warn("Unknown endpoint type")
		success = true //Prevent Retry for bad message
	}
	return success
}

func (t *Tracker) sendDeploymentEventHTTP(endpoint string, m DeployMessage) bool {
	jsonData, err := json.Marshal(m.Body)
	if err != nil {
		log.Errorf("Failed marshalling the deployment object: %v", err)
		return false
	}
	resp, err := t.httpClient.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Errorf("failed to send deployment event to %s: %v", endpoint, err)
		return false
	}
	if resp.StatusCode >= 400 {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		log.Infof("response: %v", result)
		resp.Body.Close()
		return false
	}
	resp.Body.Close()
	return true

}

func (t *Tracker) sendDeploymentEventSQS(endpoint string, m DeployMessage) bool {
	messageType := m.Type
	messageVersion := m.Version
	jsonData, err := json.Marshal(m.Body)
	if err != nil {
		log.Errorf("Failed marshalling the deployment object: %v", err)
		return false
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
				StringValue: aws.String(messageVersion),
			},
		},
		MessageBody: aws.String(string(jsonData[:])),
		QueueUrl:    aws.String(endpoint),
	})
	if err != nil {
		log.Errorf("Failed sending the deployment object: %v", err)
		return false
	}
	return true
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
