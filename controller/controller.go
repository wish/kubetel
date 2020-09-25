package controller

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	clientset "github.com/wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	kcdutil "github.com/wish/kubetel/gok8s/kcdutil"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	batchv1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

//Controller object
type Controller struct {
	k8sClient kubernetes.Interface
	customCS  clientset.Interface

	kcdcInformer cache.SharedIndexInformer
	kcdcSynced   cache.InformerSynced

	jobcInformer cache.SharedIndexInformer
	jobcSynced   cache.InformerSynced

	k8sIF k8sinformers.SharedInformerFactory

	batchClient batchv1Client.BatchV1Interface

	queue     workqueue.RateLimitingInterface
	kcdStates map[string]string

	endpointMap map[string]string

	jobDeleteStatus map[string]chan int
	sync.Mutex

	jobCleaner JobCleaner
}

//NewController Creates a new deyployment controller
func NewController(k8sClient kubernetes.Interface, customCS clientset.Interface, customIF informer.SharedInformerFactory, k8sIF k8sinformers.SharedInformerFactory) (*Controller, error) {

	kcdInformer := customIF.Custom().V1().KCDs()
	jobInformer := k8sIF.Batch().V1().Jobs()
	batchClient := k8sClient.BatchV1()

	c := &Controller{
		kcdcInformer:    kcdInformer.Informer(),
		jobcInformer:    jobInformer.Informer(),
		customCS:        customCS,
		kcdcSynced:      kcdInformer.Informer().HasSynced,
		jobcSynced:      jobInformer.Informer().HasSynced,
		jobDeleteStatus: make(map[string](chan int)),
		k8sIF:           k8sIF,
		batchClient:     batchClient,
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployController"),
		kcdStates:       make(map[string]string),
		endpointMap:     viper.GetStringMapString("tracker.appendpoints"),
	}

	c.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddKcd,
		UpdateFunc: c.trackKcd,
	})

	c.jobcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteJob,
	})
	c.jobCleaner = *NewJobCleaner(c.jobcInformer, c.batchClient)

	return c, nil
}

//Adds a new job request into the work queue
func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {

		runtime.HandleError(fmt.Errorf("error obtaining key for object being enqueue: %s", err.Error()))
		log.Errorf("Failed to obtain key for object being enqueue: %v", err)
		return
	}
	log.Tracef("Enququing object with key: %s", key)
	c.queue.AddRateLimited(key)
}

// Run starts the controller
func (c *Controller) Run(threadCount int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Controller")

	go c.kcdcInformer.Run(stopCh)
	go c.jobcInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.kcdcSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}
	if !cache.WaitForCacheSync(stopCh, c.jobcSynced) {
		return errors.New("Fail to wait for (secondary) cache sync")
	}

	log.Info("Cache sync completed")

	for i := 0; i < threadCount; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	log.Info("Started Controller")

	<-stopCh
	log.Info("Shutting down container version controller")
	return nil
}

func (c *Controller) runWorker() {
	log.Info("Running worker")
	for c.processNextItem() {
	}
}

//dequeues workqueue with retry if failed
func (c *Controller) processNextItem() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	c.queue.Done(key)

	err := c.processItem(key.(string))

	maxRetries := viper.GetInt("controller.maxretries")

	if err == nil {
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		log.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		runtime.HandleError(err)
	}

	return true
}

//Process the next KCD in the queue
func (c *Controller) processItem(key string) error {
	obj, exists, err := c.kcdcInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("Object does not exists")
	}
	kcd, ok := obj.(*customv1.KCD)
	if !ok {
		return err
	}
	log.Infof("Deququing KCD: %s ", kcd.Name)

	namespace, _, err := cache.SplitMetaNamespaceKey(key)
	name := kcd.Spec.Selector["kcdapp"]
	version := kcd.Status.CurrVersion
	jobName := fmt.Sprintf("deploy-tracker-%s-%s", name, version)

	//Delete existing job if it already completed
	jobsClient := c.batchClient.Jobs(namespace)
	var gracePeriodSeconds int64
	var propagationPolicy metav1.DeletionPropagation = metav1.DeletePropagationForeground
	exJob, err := jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		for _, condition := range exJob.Status.Conditions {
			if (condition.Type == "Complete" || condition.Type == "Failed") &&
				condition.Status == "True" {
				//Here we pass map the jobname we want to delelete to a channel
				//Once the jobcInformer get the deletion event it will write to the channel
				jobDeleted := make(chan int)
				c.Lock()
				c.jobDeleteStatus[jobName] = jobDeleted
				c.Unlock()
				err = jobsClient.Delete(jobName, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &propagationPolicy})
				if err != nil {
					//If this fails, the job probably was deleted someone else before we got the chance
					log.Warnf("failed to delete job %s with error: %s", jobName, err)
				} else {
					//Otherwise wiat for the deletion conformation
					<-jobDeleted
				}
				//Cleanup
				c.Lock()
				delete(c.jobDeleteStatus, jobName)
				c.Unlock()
				close(jobDeleted)
			}
		}
	}

	arr := []string{"kubetel", "kube-deploy", "merchant-backend-spawn"}

	for _, v := range arr {
		if v == name {
			log.Infof("Blaclisted project, skipping %s......", v)
			return
		}
	}

	//Check if there is a specific endpoint override for the app else use default
	var kubeDeployEndpoint string
	if kubeDeployEndpoint, ok = c.endpointMap[name]; !ok {
		kubeDeployEndpoint = viper.GetString("tracker.kubedeploy_sqs_endpoint")
	}
	// This is configured based on env, no need to hack
	robbieEndpoint := viper.GetString("tracker.robbie_sqs_endpoint")


	args := []string{"tracker",
		fmt.Sprintf("--cluster=%s", viper.GetString("cluster")),
		fmt.Sprintf("--sqsregion=%s", viper.GetString("sqsregion")),
		fmt.Sprintf("--log=%s", viper.GetString("log.level")),
		fmt.Sprintf("--use-config=false"),
		fmt.Sprintf("--tracker-kcdapp=%s", kcd.Spec.Selector["kcdapp"]),
		fmt.Sprintf("--tracker-version=%s", version),
		fmt.Sprintf("--tracker-namespace=%s", namespace),
		fmt.Sprintf("--tracker-endpointtype=%s", viper.GetString("tracker.endpointtype")),
		fmt.Sprintf("--tracker-kubedeploy-endpoint=%s", kubeDeployEndpoint),
		fmt.Sprintf("--tracker-robbie-endpoint=%s", robbieEndpoint),
		fmt.Sprintf("--tracker-maxretries=%d", viper.GetInt("tracker.maxretries")),
		fmt.Sprintf("--tracker-workercount=%d", viper.GetInt("tracker.workercount")),
		fmt.Sprintf("--server-port=%d", viper.GetInt("server.port")),
	}

	//Number of time job will retry upon failure
	var backoffLimit int32 = 10

	log.Infof("Creating job for : %s", jobName)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: jobName,
				},
				Spec: corev1.PodSpec{
					NodeSelector: viper.GetStringMapString("nodeSelector"),
					Containers: []corev1.Container{
						{
							Name:            fmt.Sprintf("%s-container", jobName),
							Image:           viper.GetString("image"),
							Args:            args,
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 443,
									Protocol:      "TCP",
								},
								{
									Name:          "http",
									ContainerPort: 80,
									Protocol:      "TCP",
								},
							},
						},
					},
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "kubetel-tracker",
				},
			},
			BackoffLimit: &backoffLimit,
		},
	}
	_, err = jobsClient.Create(job)
	if err != nil {
		//Occasionally this is caused by a race condition and is fine
		if strings.Contains(err.Error(), "already exists") {
			log.Infof("Job : %s : already exists, skipping", jobName)
			return nil
		}

		return errors.Wrapf(err, "Failed to create job %s", job.Name)
	}
	return nil
}

//Handler for kcdcInformer Update
func (c *Controller) trackKcd(oldObj interface{}, newObj interface{}) {
	//Check that we actully have KCD objects
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	oldKCD, ok := oldObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}

	log.Infof("oldKCD status: %v vs newKCD status: %v", oldKCD.Status.CurrStatus, newKCD.Status.CurrStatus)
	log.Infof("in-memory kcdStates: %v", c.kcdStates)
	if oldKCD.Status.CurrStatus == newKCD.Status.CurrStatus {
		log.Info("oldKCD and newKCD status unchanged")
		return
	}
	if newKCD.Status.CurrStatus == c.kcdStates[newKCD.Name] {
		log.Info("newKCD and in-memory kcdStates status unchanged")
		return
	}
	if newKCD.Status.CurrStatus == kcdutil.StatusProgressing && c.kcdStates[newKCD.Name] != kcdutil.StatusProgressing {
		c.enqueue(newObj)
	}
	//We need to keep track of the last known state in kubetel because most of the time
	//the api server will bundle multiple events into one update and the oldObj and newObj
	//will be identical
	c.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus

}

//Handler for kcdcInformer Add
//This occours if the tracker crashes and restarts to find a completed deployment
func (c *Controller) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*customv1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	c.kcdStates[newKCD.Name] = newKCD.Status.CurrStatus
	if newKCD.Status.CurrStatus == kcdutil.StatusProgressing {
		c.enqueue(newObj)
	}
}

//Handler for JobcInformer Delete
func (c *Controller) deleteJob(obj interface{}) {
	job, ok := obj.(*batchv1.Job)
	if !ok {
		log.Errorf("Not a job object")
		return
	}
	var ch chan int

	//If there is a chan mapped to this jobname it then a worker thread
	//Is waiting for a delete conformation and by writing to the the mapped
	//chan
	c.Lock()
	ch, ok = c.jobDeleteStatus[job.Name]
	if ok {
		ch <- 1
	}
	c.Unlock()
}
