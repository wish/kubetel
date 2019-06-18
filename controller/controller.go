package controller

import (
	"fmt"
	"reflect"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/cache"
)

//Controller object
type Controller struct {
	deploymentInformer cache.SharedIndexInformer
	kcdcInformer       cache.SharedIndexInformer
	MessageQ           chan string
	MessageQ2          chan string
}

//NewController Creates a new deyployment controller
func NewController(deployInformer, kcdcInformer cache.SharedIndexInformer, q, q2 chan string) (*Controller, error) {
	c := &Controller{
		deploymentInformer: deployInformer,
		kcdcInformer:       kcdcInformer,
		MessageQ:           q,
		MessageQ2:          q2,
	}
	c.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddDeployment,
		UpdateFunc: c.trackDeployment,
		DeleteFunc: c.trackDeleteDeployment,
	})
	kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddKcd,
		UpdateFunc: c.trackKcd,
		DeleteFunc: c.trackDeleteKcd,
	})
	stopper := make(chan struct{})
	go c.deploymentInformer.Run(stopper)
	go c.kcdcInformer.Run(stopper)
	return c, nil
}

func (c *Controller) trackDeployment(oldObj interface{}, newObj interface{}) {
	if !reflect.DeepEqual(oldObj, newObj) {
		newDeploy, ok := newObj.(*appsv1.Deployment)
		if !ok {
			glog.Errorf("Not a deploy object")
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
		c.MessageQ <- fmt.Sprintln("UPDATE DEPLOY: ", name)
		c.MessageQ <- fmt.Sprintln(newDeploy)
		c.MessageQ <- fmt.Sprintln(oldDeploy)
	}
}

func (c *Controller) trackAddDeployment(obj interface{}) {
	newDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		glog.Errorf("Not a deploy object")
		return
	}
	name, ok := newDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	c.MessageQ <- fmt.Sprintln("ADD DEPLOY: ", name)

}

func (c *Controller) trackDeleteDeployment(obj interface{}) {
	oldDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		glog.Errorf("Not a deploy object")
		return
	}
	name, ok := oldDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	c.MessageQ <- fmt.Sprintln("DELETE DEPLOY: ", name)
}

func (c *Controller) trackKcd(oldObj interface{}, newObj interface{}) {
	if !reflect.DeepEqual(oldObj, newObj) {
		newKCD, ok := newObj.(*v1.KCD)
		if !ok {
			glog.Errorf("Not a KCD object")
			return
		}
		oldKCD, ok := oldObj.(*v1.KCD)
		if !ok {
			glog.Errorf("Not a KCD object")
			return
		}
		if oldKCD.Status.CurrStatus == newKCD.Status.CurrStatus {
			return
		}
		c.MessageQ2 <- fmt.Sprintln("UPDATE KCD: ", newKCD.Name)
		c.MessageQ2 <- fmt.Sprintln(oldKCD.Status.CurrStatus)
		c.MessageQ2 <- fmt.Sprintln(newKCD.Status.CurrStatus)
	}
}
func (c *Controller) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	c.MessageQ2 <- fmt.Sprintln("ADD KCD: ", newKCD.Name)
	c.MessageQ2 <- fmt.Sprintln(newKCD.Status.CurrStatus)
}

func (c *Controller) trackDeleteKcd(oldObj interface{}) {
	oldKCD, ok := oldObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	c.MessageQ2 <- fmt.Sprintln("DELETE KCD: ", oldKCD.Name)
	c.MessageQ2 <- fmt.Sprintln(oldKCD.Status.CurrStatus)
}
