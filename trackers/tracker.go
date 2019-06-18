package trackers

import (
	"fmt"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"
	"github.com/golang/glog"

	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/tools/cache"
)

type Syncer struct {
	deploymentInformer cache.SharedIndexInformer
	kcdcInformer       cache.SharedIndexInformer
	MessageQ           chan string
	MessageQ2          chan string
}

//hi
func NewSyncer(deployInformer, kcdcInformer cache.SharedIndexInformer, q, q2 chan string) (*Syncer, error) {
	s := &Syncer{
		deploymentInformer: deployInformer,
		kcdcInformer:       kcdcInformer,
		MessageQ:           q,
		MessageQ2:          q2,
	}
	s.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.trackAddDeployment,
		UpdateFunc: s.trackDeployment,
		DeleteFunc: s.trackDeleteDeployment,
	})
	kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.trackAddKcd,
		UpdateFunc: s.trackKcd,
		DeleteFunc: s.trackDeleteKcd,
	})
	stopper := make(chan struct{})
	go s.deploymentInformer.Run(stopper)
	go s.kcdcInformer.Run(stopper)
	return s, nil
}

func (s *Syncer) trackDeployment(oldObj interface{}, newObj interface{}) {
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
		s.MessageQ <- fmt.Sprintln("UPDATE DEPLOY: ", name)
		s.MessageQ <- fmt.Sprintln(newDeploy)
		s.MessageQ <- fmt.Sprintln(oldDeploy)
	}
}

func (s *Syncer) trackAddDeployment(obj interface{}) {
	newDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		glog.Errorf("Not a deploy object")
		return
	}
	name, ok := newDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	s.MessageQ <- fmt.Sprintln("ADD DEPLOY: ", name)
	s.MessageQ <- fmt.Sprintln(newDeploy)

}

//YEET
func (s *Syncer) trackDeleteDeployment(obj interface{}) {
	oldDeploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		glog.Errorf("Not a deploy object")
		return
	}
	name, ok := oldDeploy.Labels["kcdapp"]
	if !ok {
		return
	}
	s.MessageQ <- fmt.Sprintln("DELETE DEPLOY: ", name)
}

func (s *Syncer) trackKcd(oldObj interface{}, newObj interface{}) {
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
		s.MessageQ2 <- fmt.Sprintln("UPDATE KCD: ", newKCD.Name)
		s.MessageQ2 <- fmt.Sprintln(oldKCD.Status.CurrStatus)
		s.MessageQ2 <- fmt.Sprintln(newKCD.Status.CurrStatus)
	}
}
func (s *Syncer) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	s.MessageQ2 <- fmt.Sprintln("ADD KCD: ", newKCD.Name)
	s.MessageQ2 <- fmt.Sprintln(newKCD.Status.CurrStatus)
}

func (s *Syncer) trackDeleteKcd(oldObj interface{}) {
	oldKCD, ok := oldObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	s.MessageQ2 <- fmt.Sprintln("DELETE KCD: ", oldKCD.Name)
	s.MessageQ2 <- fmt.Sprintln(oldKCD.Status.CurrStatus)
}
