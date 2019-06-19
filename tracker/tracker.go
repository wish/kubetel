package tracker

import (
	"fmt"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"
	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/cache"
)

type Tracker struct {
	deploymentInformer cache.SharedIndexInformer
	kcdcInformer       cache.SharedIndexInformer
	MessageQ           chan string
}

//hi
func NewTracker(deployInformer, kcdcInformer cache.SharedIndexInformer, q chan string) (*Tracker, error) {
	t := &Tracker{
		deploymentInformer: deployInformer,
		kcdcInformer:       kcdcInformer,
		MessageQ:           q,
	}
	t.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddDeployment,
		UpdateFunc: t.trackDeployment,
		DeleteFunc: t.trackDeleteDeployment,
	})
	t.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddKcd,
		UpdateFunc: t.trackKcd,
		DeleteFunc: t.trackDeleteKcd,
	})
	stopper := make(chan struct{})
	go t.deploymentInformer.Run(stopper)
	go t.kcdcInformer.Run(stopper)
	return t, nil
}

func (t *Tracker) trackDeployment(oldObj interface{}, newObj interface{}) {
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
		t.MessageQ <- fmt.Sprintln("UPDATE DEPLOY: ", name)
		t.MessageQ <- fmt.Sprintln(newDeploy)
		t.MessageQ <- fmt.Sprintln(oldDeploy)
	}
}

func (t *Tracker) trackAddDeployment(obj interface{}) {
	newDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		log.Errorf("Not a deploy object")
		return
	}
	name, ok := newDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	t.MessageQ <- fmt.Sprintln("ADD DEPLOY: ", name)
	t.MessageQ <- fmt.Sprintln(newDeploy)

}

func (t *Tracker) trackDeleteDeployment(obj interface{}) {
	oldDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		log.Errorf("Not a deploy object")
		return
	}
	name, ok := oldDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	t.MessageQ <- fmt.Sprintln("DELETE DEPLOY: ", name)
}

func (t *Tracker) trackKcd(oldObj interface{}, newObj interface{}) {
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
		t.MessageQ <- fmt.Sprintln("UPDATE KCD: ", newKCD.Name)
		t.MessageQ <- fmt.Sprintln(oldKCD.Status.CurrStatus)
		t.MessageQ <- fmt.Sprintln(newKCD.Status.CurrStatus)
	}
}
func (t *Tracker) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	t.MessageQ <- fmt.Sprintln("ADD KCD: ", newKCD.Name)
	t.MessageQ <- fmt.Sprintln(newKCD.Status.CurrStatus)
}

func (t *Tracker) trackDeleteKcd(oldObj interface{}) {
	oldKCD, ok := oldObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	t.MessageQ <- fmt.Sprintln("DELETE KCD: ", oldKCD.Name)
	t.MessageQ <- fmt.Sprintln(oldKCD.Status.CurrStatus)
}
