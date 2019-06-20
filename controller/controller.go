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

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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
	customCS     clientset.Interface
	kcdcInformer cache.SharedIndexInformer
	kcdcSynced   cache.InformerSynced
	queue        workqueue.RateLimitingInterface
}

//NewController Creates a new deyployment controller
func NewController(customCS clientset.Interface, customIF informer.SharedInformerFactory) (*Controller, error) {

	kcdInformer := customIF.Custom().V1().KCDs()

	c := &Controller{
		kcdcInformer: kcdInformer.Informer(),
		customCS:     customCS,
		kcdcSynced:   kcdInformer.Informer().HasSynced,
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
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.queue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.queue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in queue but got %#v", obj))
			return nil
		}

		if err := c.processItem(key); err != nil {
			return errors.Wrapf(err, "error syncing '%s'", key)
		}

		c.queue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) processItem(key string) error {
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
