package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	kcdutil "github.com/wish/kubetel/gok8s/kcdutil"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	log "github.com/sirupsen/logrus"
)

//Tracker holds comonents needed to track a deployment
type Tracker struct {
	deploymentcInformer cache.SharedIndexInformer
	kcdcInformer        cache.SharedIndexInformer

	k8scSynced cache.InformerSynced
	kcdcSynced cache.InformerSynced

	deployMessageQueue     chan DeployMessage
	deployMessageQueueDone int32
	deployMessageQueueWG   sync.WaitGroup

	informerList   []string
	informerQueues map[string]chan DeployMessage

	deploymentClient appsclientv1.DeploymentInterface
	podClient        coreclientv1.PodInterface

	httpClient *http.Client
	rand       *rand.Rand

	sqsClient *sqs.SQS

	clusterName             string
	version                 string
	deployStatusEndpointAPI string
	kcdStates               map[string]string

	namespace            string
	sqsregion            string
	endpointendpointtype string
	kcdapp               string
}

//Config holds cracker configuration data
type Config struct {
	Namespace            string
	SQSregion            string
	Endpointendpointtype string
	Cluster              string
	Version              string
	Endpoint             string
	KCDapp               string
}

//NewTracker Creates a new tracker
func NewTracker(k8sClient kubernetes.Interface, customIF informer.SharedInformerFactory, k8sIF k8sinformers.SharedInformerFactory, c Config) (*Tracker, error) {

	//Each informer write to their own queue which are then merged
	informerQueues := make(map[string]chan DeployMessage)

	//Create all required Informer (Also add additional ones here)
	kcdInformer := customIF.Custom().V1().KCDs()
	deploymentInformer := k8sIF.Apps().V1().Deployments()

	//If you add an informer add a key here, the order of the keys will be the order they are emptied
	informerList := []string{"kcd", "deployment"}
	for _, informer := range informerList {
		informerQueues[informer] = make(chan DeployMessage, 100)
	}

	deploymentClient := k8sClient.AppsV1().Deployments(c.Namespace)
	podClient := k8sClient.CoreV1().Pods(viper.GetString("tracker.namespace"))

	//Set up message endpoint
	var httpClient *http.Client
	var rander *rand.Rand
	var sqsClient *sqs.SQS

	switch c.Endpointendpointtype {
	case "http":
		if c.Endpoint != "" {
			httpClient = &http.Client{
				Timeout: time.Duration(5 * time.Second),
			}
			rander = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
	case "sqs":
		sess := session.Must(session.NewSession(&aws.Config{
			Region: aws.String(c.SQSregion),
		}))
		sqsClient = sqs.New(sess)
	default:
		err := errors.New("Unknown endpoint type given: " + c.Endpointendpointtype)
		return nil, err
	}

	t := &Tracker{
		deploymentcInformer: deploymentInformer.Informer(),
		kcdcInformer:        kcdInformer.Informer(),

		deploymentClient: deploymentClient,
		podClient:        podClient,

		k8scSynced: deploymentInformer.Informer().HasSynced,
		kcdcSynced: kcdInformer.Informer().HasSynced,

		deployMessageQueue: make(chan DeployMessage, 100),

		informerQueues: informerQueues,
		informerList:   informerList,

		clusterName:             c.Cluster,
		version:                 c.Version,
		deployStatusEndpointAPI: c.Endpoint,
		namespace:               c.Namespace,
		sqsregion:               c.SQSregion,
		endpointendpointtype:    c.Endpointendpointtype,
		kcdapp:                  c.KCDapp,

		httpClient: httpClient,
		rand:       rander,
		sqsClient:  sqsClient,
		kcdStates:  make(map[string]string),
	}

	//Add event handlers to Informers
	t.deploymentcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: t.trackDeployment,
	})
	t.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddKcd,
		UpdateFunc: t.trackKcd,
	})
	return t, nil
}

//If the given Object is a deploymnet that belongs to the KCD being tracked send deploy message
func (t *Tracker) trackDeployment(oldObj interface{}, newObj interface{}) {
	newDeploy, ok := newObj.(*appsv1.Deployment)
	if !ok {
		log.Errorf("Not a deploy object")
		return
	}
	oldDeploy, ok := oldObj.(*appsv1.Deployment)
	if !ok {
		log.Errorf("Not a deploy object")
		return
	}
	name, ok := oldDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	if name == t.kcdapp {
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
		t.enqueue(t.informerQueues["deployment"], deployMessage)
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

	//oldObj and newObj are the same most of the time because of how the k8s API server bundles updates
	t.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if name, ok := newKCD.Spec.Selector["kcdapp"]; !ok || name != t.kcdapp {
		log.Tracef("%s:%s", newKCD.Spec.Selector["kcdapp"], t.kcdapp)
		return
	}

	if podStatus == kcdutil.StatusSuccess || podStatus == kcdutil.StatusFailed {
		t.kcdFinish(newKCD)
	}
}

func (t *Tracker) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}

	podStatus := newKCD.Status.CurrStatus
	log.Tracef("KCD with status: %s", podStatus)

	//oldObj and newObj are the same most of the time because of how the k8s API server bundles updates
	t.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if name, ok := newKCD.Spec.Selector["kcdapp"]; !ok || name != t.kcdapp {
		log.Tracef("%s:%s", newKCD.Spec.Selector["kcdapp"], t.kcdapp)
		return
	}
	//This happens when a tracker is spawned and the job is already finished (this should not happen but he handle it if it does)
	if podStatus == kcdutil.StatusSuccess || podStatus == kcdutil.StatusFailed {
		t.kcdFinish(newKCD)
	}
}

func (t *Tracker) kcdFinish(kcd *customv1.KCD) {
	t.deployFinishHandler(kcd)
	log.Info("Finished sending ")
	//Do not double close channels
	if atomic.LoadInt32(&t.deployMessageQueueDone) == int32(0) {
		atomic.StoreInt32(&t.deployMessageQueueDone, int32(1))
		//Close all informer channels after
		for _, informer := range t.informerList {
			close(t.informerQueues[informer])
		}
	}
}

//Send all finish events
func (t *Tracker) deployFinishHandler(kcd *customv1.KCD) {
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

	//Send Additional Information if the deployment fails
	if !success {
		t.deployFailureHandler(kcd, deployments)
	}

	//For each Kubernets deployemnt tracked by a KCD send a status for each
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
		t.enqueue(t.informerQueues["kcd"], deployMessage)
	}

}

//Grab logs of pods in a failed deployment
func (t *Tracker) deployFailureHandler(kcd *customv1.KCD, deployments *appsv1.DeploymentList) {
	for _, item := range deployments.Items {
		deployment := item
		set := labels.Set(deployment.Spec.Selector.MatchLabels)
		pods, err := t.podClient.List(metav1.ListOptions{LabelSelector: set.AsSelector().String()})
		if err != nil {
			log.Warnf("Unable to grab pod logs for deployment: " + deployment.Name)
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase != "Running" {
				for _, container := range pod.Spec.Containers {
					logs, err := t.getContainerLog(pod.Name, container.Name)
					if err != nil {
						log.Warn(err)
					}
					log.Info(logs)
					deployMessage := DeployMessage{
						Type:    "deployFailedLogs",
						Version: "v1alpha1",
						Body: FailedPodLogData{
							t.clusterName,
							time.Now().UTC(),
							deployment,
							t.version,
							pod.Name,
							container.Name,
							logs,
						},
					}
					t.enqueue(t.informerQueues["kcd"], deployMessage)
				}
			}
		}

	}
	return
}

func (t *Tracker) getContainerLog(podName, containerName string) (string, error) {
	req := t.podClient.GetLogs(podName, &corev1.PodLogOptions{Container: containerName})

	readCloser, err := req.Stream()
	if err != nil {
		msg := fmt.Sprintf("Failed to get logs from %s/%s", podName, containerName)
		return msg, errors.Wrap(err, msg)
	}
	defer readCloser.Close()
	body, err := ioutil.ReadAll(readCloser)
	if err != nil {
		msg := fmt.Sprintf("Failed to parse logs from %s/%s", podName, containerName)
		return msg, errors.Wrap(err, msg)
	}
	logs := string(body)
	if len(logs) > 200000 {
		logs = logs[len(logs)-200000:]
	}
	return logs, nil
}

//Adds a new job request into the work queue without
func (t *Tracker) enqueue(ichan chan DeployMessage, m DeployMessage) {
	if atomic.LoadInt32(&t.deployMessageQueueDone) == int32(0) {
		ichan <- m
	}
}

//InformerQueueMerger empties the informer queues into the main work queue
func (t *Tracker) InformerQueueMerger() {
	var wg sync.WaitGroup
	for _, informer := range t.informerList {
		c := t.informerQueues[informer]
		wg.Add(1)
		go func() {
			for m := range c {
				t.deployMessageQueueWG.Add(1)
				t.deployMessageQueue <- m
			}
			wg.Done()
		}()
	}
	wg.Wait() //Wait for all informer channels to close (end of deployment)
	time.Sleep(time.Second)
	t.deployMessageQueueWG.Wait() //Wait for all remaining messages to be sent
	log.Info("closing main chan")
	close(t.deployMessageQueue)
}

// Run starts the tracker
func (t *Tracker) Run(threadCount int, stopCh <-chan struct{}, waitgroup *sync.WaitGroup) error {
	defer runtime.HandleCrash()

	log.Info("Starting Tracker")
	go t.InformerQueueMerger()
	go t.kcdcInformer.Run(stopCh)
	go t.deploymentcInformer.Run(stopCh)

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
	waitgroup.Wait()
	log.Info("Shutting down tracker")
	return nil
}

func (t *Tracker) runWorker() {
	log.Info("Running worker")
	for m := range t.deployMessageQueue {
		log.Info("Dequeue Message")
		success := t.processNextItem(m)
		if !success {
			m.Retries++
			time.Sleep(t.sleepDuration(m.Retries))
			t.deployMessageQueue <- m
		}
	}
}

func (t *Tracker) processNextItem(data DeployMessage) (success bool) {
	switch endtype := t.endpointendpointtype; endtype {
	//Send message to correct http endpoint
	case "http":
		switch messageType := data.Type; messageType {
		case "deployFinished":
			success = t.sendDeploymentEventHTTP(fmt.Sprintf("%s/finished", t.deployStatusEndpointAPI), data)
		case "deployStatus":
			success = t.sendDeploymentEventHTTP(t.deployStatusEndpointAPI, data)
		case "deployFailedLogs":
			success = t.sendDeploymentEventHTTP(fmt.Sprintf("%s/podlogs", t.deployStatusEndpointAPI), data)
		default:
			log.WithFields(log.Fields{"message_type": messageType}).Warn("Unknown message type for http endpoint")
			success = true //Prevent Retry for bad message
		}
	//Send to sqs
	case "sqs":
		success = t.sendDeploymentEventSQS(t.deployStatusEndpointAPI, data)
	default:
		log.WithFields(log.Fields{"endpoint_type": endtype}).Fatal("Unknown endpoint type")
		success = true //Prevent Retry for bad message
	}
	if success {
		t.deployMessageQueueWG.Done()
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
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		log.Infof("Failed to send with response: %v", buf.String())
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
	log.Tracef("Sending: deploy message %s", messageType)
	log.Tracef("BODY: %s", string(jsonData[:]))
	_, err = t.sqsClient.SendMessage(&sqs.SendMessageInput{
		MessageAttributes: map[string]*sqs.MessageAttributeValue{
			"Type": {
				DataType:    aws.String("String"),
				StringValue: aws.String(messageType),
			},
			"Version": {
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

//Generates a time to sleep for based on how many retures there have been
func (t *Tracker) sleepDuration(attempts int) time.Duration {
	sleepTime := (t.rand.Float64() + 1) + math.Pow(2, float64(attempts-0))
	durationStr := fmt.Sprintf("%ss", strconv.FormatFloat(sleepTime, 'f', 2, 64))
	sleepDuration, _ := time.ParseDuration(durationStr)
	return sleepDuration
}
