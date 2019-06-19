package controller

import (
	"fmt"
	"reflect"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/cache"
)

//Controller object
type Controller struct {
	deploymentInformer cache.SharedIndexInformer
	kcdcInformer       cache.SharedIndexInformer
	MessageQ           chan string
	MessageQ2          chan string
}

const (
	StatusFailed      = "Failed"
	StatusSuccess     = "Success"
	StatusProgressing = "Progressing"
)

//NewController Creates a new deyployment controller
func NewController(kcdcInformer cache.SharedIndexInformer) (*Controller, error) {
	c := &Controller{
		kcdcInformer: kcdcInformer,
	}
	kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.trackAddKcd,
		UpdateFunc: c.trackKcd,
	})
	stopper := make(chan struct{})
	go c.kcdcInformer.Run(stopper)
	return c, nil
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
			fmt.Println("Spawn new tracker for ", newKCD.Name)
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
		fmt.Println("Spawn new tracker for ", newKCD.Name)
	}
}
