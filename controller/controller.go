package controller

import (
	"fmt"
	"reflect"
	"time"

	clientset "github.com/Wish/kubetel/gok8s/client/clientset/versioned"
	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	batchv1Client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	StatusFailed      = "Failed"
	StatusSuccess     = "Success"
	StatusProgressing = "Progressing"
)

//Controller object
type Controller struct {
	k8sClient kubernetes.Interface
	customCS  clientset.Interface

	kcdcInformer cache.SharedIndexInformer
	kcdcSynced   cache.InformerSynced

	batchClient batchv1Client.BatchV1Interface

	ktImgRepo string

	queue workqueue.RateLimitingInterface
}

//NewController Creates a new deyployment controller
func NewController(ktImgRepo string, k8sClient kubernetes.Interface, customCS clientset.Interface, customIF informer.SharedInformerFactory) (*Controller, error) {

	kcdInformer := customIF.Custom().V1().KCDs()
	batchClient := k8sClient.BatchV1()

	c := &Controller{
		kcdcInformer: kcdInformer.Informer(),
		customCS:     customCS,
		kcdcSynced:   kcdInformer.Informer().HasSynced,
		batchClient:  batchClient,
		ktImgRepo:    ktImgRepo,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployController"),
	}
	c.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddKcd,
		UpdateFunc: c.trackKcd,
	})
	return c, nil
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		runtime.HandleError(fmt.Errorf("error obtaining key for object being enqueue: %s", err.Error()))
		log.Errorf("Failed to obtain key for object being enqueue: %v", err)
		return
	}
	c.queue.AddRateLimited(key)
}

// Run starts the controller
func (c *Controller) Run(threadCount int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("StartingController")

	go c.kcdcInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.kcdcSynced) {
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

//Processes items from the queue
func (c *Controller) runWorker() {
	log.Info("Running worker")
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	key, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(key)

	err := c.processItem(key.(string))

	//TODO: move to config file
	maxRetries := 2

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

func (c *Controller) processItem(key string) error {
	obj, exists, err := c.kcdcInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("object no longer exists")
	}
	kcd, ok := obj.(*v1.KCD)
	if !ok {
		return err
	}
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	version := kcd.Status.CurrVersion
	jobName := fmt.Sprintf("kubedeploy-tracker-%s-%s-%s", namespace, name, version)

	jobsClient := c.batchClient.Jobs(namespace)

	_, err = jobsClient.Get(jobName, metav1.GetOptions{})
	if err == nil {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: "kuebdeploy",
			},
			Spec: batchv1.JobSpec{
				Template: apiv1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name: jobName,
					},
					Spec: apiv1.PodSpec{
						Containers: []apiv1.Container{
							{
								Name:  fmt.Sprintf("%s-container", jobName),
								Image: "yourimage",
							},
						},
						RestartPolicy: apiv1.RestartPolicyOnFailure,
					},
				},
			},
		}
		_, err := jobsClient.Create(job)
		if err != nil {
			return errors.Wrapf(err, "Failed to create job %s", job.Name)
		}
	}
	log.Infof("Job : %s : already exists, skipping", jobName)
	return nil
}

func (c *Controller) trackKcd(oldObj interface{}, newObj interface{}) {
	if !reflect.DeepEqual(oldObj, newObj) {
		newKCD, ok := newObj.(*v1.KCD)
		if !ok {
			log.Errorf("Not a KCD object")
			return
		}
		oldKCD, ok := oldObj.(*v1.KCD)
		if !ok {
			log.Errorf("Not a KCD object")
			return
		}
		if oldKCD.Status.CurrStatus == newKCD.Status.CurrStatus {
			return
		}
		if newKCD.Status.CurrStatus == StatusProgressing {
			c.enqueue(newObj)
		}
	}
}
func (c *Controller) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*v1.KCD)
	if !ok {
		log.Errorf("Not a KCD object")
		return
	}
	if newKCD.Status.CurrStatus == StatusProgressing {
		c.enqueue(newObj)
	}
}
