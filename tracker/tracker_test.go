package tracker_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/wish/kubetel/signals"
	tracker "github.com/wish/kubetel/tracker"

	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	customclientfake "github.com/wish/kubetel/gok8s/client/clientset/versioned/fake"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sinformers "k8s.io/client-go/informers"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"
)

func TestDeployUpdateHTTP(t *testing.T) {
	stopChan := make(chan struct{})
	kcdapp := "testapp1"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		var s = new(tracker.StatusData)
		err := json.Unmarshal(b, &s)
		if err != nil {
			t.Errorf("Did not get a Status Data response, got: %s", string(b))
		}
		if s.Deploy.Labels["kcdapp"] != kcdapp {
			t.Errorf("Got Status Data for app that was not expected: %s", s.Deploy.Labels["kcdapp"])
		}
		close(stopChan)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	c := tracker.Config{
		Namespace:            "testing",
		SQSregion:            "N/A",
		Endpointendpointtype: "http",
		Cluster:              "cluster-test",
		Version:              "2222222",
		Endpoint:             ts.URL,
		KCDapp:               kcdapp,
	}

	deploy1 := generateTestingDeployment("1", "testing", "1111111")
	deploy2 := generateTestingDeployment("2", "testing", "2222222")

	customObjs := []runtime.Object{}

	k8sObjs := []runtime.Object{
		deploy1,
		deploy2,
	}

	var waitgroup sync.WaitGroup

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	k8sInformerFactory := k8sinformers.NewFilteredSharedInformerFactory(k8sClient, time.Second*30, "testing", nil) //May need additional filtering
	kcdcInformerFactory := informer.NewFilteredSharedInformerFactory(customClient, time.Second*30, "testing", nil)

	k, _ := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory, c)
	go func() {
		if err := k.Run(1, stopChan, &waitgroup); err != nil {
			log.Infof("Shutting down tracker: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)
	k8sClient.AppsV1().Deployments("testing").Update(deploy2)
	k8sClient.AppsV1().Deployments("testing").Update(deploy1)
	<-stopChan

}

func TestDeployUpdateHTTPRetry(t *testing.T) {
	stopChan := make(chan struct{})
	kcdapp := "testapp1"
	var retryCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		var s = new(tracker.StatusData)
		err := json.Unmarshal(b, &s)
		if err != nil {
			t.Errorf("Did not get a Status Data response, got: %s", string(b))
		}
		if s.Deploy.Labels["kcdapp"] != kcdapp {
			t.Errorf("Got Status Data for app that was not expected: %s", s.Deploy.Labels["kcdapp"])
		}
		retryCount++
		w.WriteHeader(http.StatusBadRequest)
		if retryCount > 1 {
			close(stopChan)
		}
	}))
	defer ts.Close()

	c := tracker.Config{
		Namespace:            "testing",
		SQSregion:            "N/A",
		Endpointendpointtype: "http",
		Cluster:              "cluster-test",
		Version:              "2222222",
		Endpoint:             ts.URL,
		KCDapp:               kcdapp,
	}

	deploy1 := generateTestingDeployment("1", "testing", "1111111")
	deploy2 := generateTestingDeployment("2", "testing", "2222222")

	customObjs := []runtime.Object{}

	k8sObjs := []runtime.Object{
		deploy1,
		deploy2,
	}

	var waitgroup sync.WaitGroup

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	k8sInformerFactory := k8sinformers.NewFilteredSharedInformerFactory(k8sClient, time.Second*30, "testing", nil) //May need additional filtering
	kcdcInformerFactory := informer.NewFilteredSharedInformerFactory(customClient, time.Second*30, "testing", nil)

	k, _ := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory, c)
	go func() {
		if err := k.Run(1, stopChan, &waitgroup); err != nil {
			log.Infof("Shutting down tracker: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)
	k8sClient.AppsV1().Deployments("testing").Update(deploy2)
	k8sClient.AppsV1().Deployments("testing").Update(deploy1)
	<-stopChan

}

func TestKCDFinishSuccess(t *testing.T) {
	stopChan := signals.SetupSignalHandler()
	kcdapp := "testapp1"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		var s = new(tracker.FinishedStatusData)
		err := json.Unmarshal(b, &s)
		if err != nil {
			t.Errorf("Did not get a Status Data response, got: %s", string(b))
		}
		if s.Deploy.Labels["kcdapp"] != kcdapp {
			t.Errorf("Got Status Data for app that was not expected: %s", s.Deploy.Labels["kcdapp"])
		}
		if !s.Success {
			t.Errorf("Wrong Status Returned")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	c := tracker.Config{
		Namespace:            "testing",
		SQSregion:            "N/A",
		Endpointendpointtype: "http",
		Cluster:              "cluster-test",
		Version:              "2222222",
		Endpoint:             ts.URL,
		KCDapp:               kcdapp,
	}

	kcd1 := generateTestingKCD("1", "testing", "1111111", "Progressing")
	kcd2 := generateTestingKCD("2", "testing", "2222222", "Progressing")

	deploy1 := generateTestingDeployment("1", "testing", "1111111")
	deploy2 := generateTestingDeployment("2", "testing", "2222222")

	customObjs := []runtime.Object{
		kcd1,
		kcd2,
	}

	k8sObjs := []runtime.Object{
		deploy1,
		deploy2,
	}

	var waitgroup sync.WaitGroup

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	k8sInformerFactory := k8sinformers.NewFilteredSharedInformerFactory(k8sClient, time.Second*30, "testing", nil) //May need additional filtering
	kcdcInformerFactory := informer.NewFilteredSharedInformerFactory(customClient, time.Second*30, "testing", nil)

	k, _ := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory, c)
	go func() {
		if err := k.Run(1, stopChan, &waitgroup); err != nil {
			log.Infof("Shutting down tracker: %v", err)
		}
	}()

	time.Sleep(2 * time.Second)
	customClient.CustomV1().KCDs("testing").Update(kcd2)
	kcd1.Status.CurrStatus = "Success"
	customClient.CustomV1().KCDs("testing").Update(kcd1)
	time.Sleep(3 * time.Second)

}

func TestKCDFinishOnStart(t *testing.T) {
	stopChan := signals.SetupSignalHandler()
	kcdapp := "testapp1"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioutil.ReadAll(r.Body)
		var s = new(tracker.FinishedStatusData)
		err := json.Unmarshal(b, &s)
		if err != nil {
			t.Errorf("Did not get a Status Data response, got: %s", string(b))
		}
		if s.Deploy.Labels["kcdapp"] != kcdapp {
			t.Errorf("Got Status Data for app that was not expected: %s", s.Deploy.Labels["kcdapp"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	c := tracker.Config{
		Namespace:            "testing",
		SQSregion:            "N/A",
		Endpointendpointtype: "http",
		Cluster:              "cluster-test",
		Version:              "1111111",
		Endpoint:             ts.URL,
		KCDapp:               kcdapp,
	}

	time.Sleep(time.Second * 2)

	kcd1 := generateTestingKCD("1", "testing", "1111111", "Success")
	deploy1 := generateTestingDeployment("1", "testing", "1111111")

	customObjs := []runtime.Object{
		kcd1,
	}

	k8sObjs := []runtime.Object{
		deploy1,
	}

	var waitgroup sync.WaitGroup

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	k8sInformerFactory := k8sinformers.NewFilteredSharedInformerFactory(k8sClient, time.Second*30, "testing", nil) //May need additional filtering
	kcdcInformerFactory := informer.NewFilteredSharedInformerFactory(customClient, time.Second*30, "testing", nil)

	k, _ := tracker.NewTracker(k8sClient, kcdcInformerFactory, k8sInformerFactory, c)
	go func() {
		if err := k.Run(1, stopChan, &waitgroup); err != nil {
			log.Infof("Shutting down tracker: %v", err)
		}
	}()
	time.Sleep(10 * time.Second)
}

func generateTestingDeployment(postfix, namespace, version string) *appsv1.Deployment {
	selector := make(map[string]string)
	selector["kcdapp"] = fmt.Sprintf("testapp%s", postfix)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kcd%s", postfix),
			Namespace: namespace,
			Labels:    selector,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
		},
	}
}

func generateTestingKCD(postfix, namespace, version, status string) *customv1.KCD {
	selector := make(map[string]string)
	selector["kcdapp"] = fmt.Sprintf("testapp%s", postfix)
	return &customv1.KCD{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kcd%s", postfix),
			Namespace: namespace,
		},
		Spec: customv1.KCDSpec{
			Config: &customv1.ConfigSpec{
				Key:  "version",
				Name: fmt.Sprintf("test-kcd%s", postfix),
			},
			Selector: selector,
		},
		Status: customv1.KCDStatus{
			CurrStatus:     status,
			CurrVersion:    version,
			SuccessVersion: version,
		},
	}
}
