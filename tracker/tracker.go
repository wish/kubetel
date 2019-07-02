package tracker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	v1 "github.com/Wish/kubetel/gok8s/apis/custom/v1"
	informer "github.com/Wish/kubetel/gok8s/client/informers/externalversions"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	k8sinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type Tracker struct {
	deploymentInformer cache.SharedIndexInformer
	kcdcInformer       cache.SharedIndexInformer

	queue workqueue.RateLimitingInterface

	httpClient *http.Client
	rand       *rand.Rand
}

//NewTracker Creates a new tracker
func NewTracker(customIF informer.SharedInformerFactory, k8sIF k8sinformers.SharedInformerFactory) (*Tracker, error) {
	t := &Tracker{
		deploymentInformer: k8sIF.Apps().V1().Deployments().Informer(),
		kcdcInformer:       customIF.Custom().V1().KCDs().Informer(),

		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "kubedeployController"),
	}
	t.deploymentInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddDeployment,
		UpdateFunc: t.trackDeployment,
	})
	t.kcdcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.trackAddKcd,
		UpdateFunc: t.trackKcd,
	})
	stopper := make(chan struct{})
	go t.deploymentInformer.Run(stopper)
	go t.kcdcInformer.Run(stopper)
	return t, nil
}

func (t *Tracker) trackDeployment(oldObj interface{}, newObj interface{}) {
	if !reflect.DeepEqual(oldObj, newObj) {
		_, ok := newObj.(*appsv1.Deployment)
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
		log.Infof("Deployment updated: %s", name)
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
	log.Infof("Deployment added: %s", name)
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
	log.Infof("Deployment deleted: %s", name)
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
		log.Infof("KCD status updated: %s", newKCD.Status.CurrStatus)

	}
}
func (t *Tracker) trackAddKcd(newObj interface{}) {
	newKCD, ok := newObj.(*v1.KCD)
	if !ok {
		glog.Errorf("Not a KCD object")
		return
	}
	log.Infof("new KCD with status: %s", newKCD.Status.CurrStatus)

}

func (t *Tracker) sendDeploymentEvent(endpoint string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		glog.Errorf("Failed marshalling the deployment object: %v", err)
		return
	}

	for retries := 0; retries < 5; retries++ {
		t.sleepFor(retries)

		resp, err := t.httpClient.Post(endpoint, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			glog.Errorf("failed to send deployment event to %s: %v", endpoint, err)
			continue
		}
		if resp.StatusCode >= 400 {
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			glog.V(4).Infof("response: %v", result)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		break
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
