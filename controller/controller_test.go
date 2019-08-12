package controller_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/wish/kubetel/controller"

	k8sinformers "k8s.io/client-go/informers"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	customclientfake "github.com/wish/kubetel/gok8s/client/clientset/versioned/fake"
	informer "github.com/wish/kubetel/gok8s/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func TestControllerUpdate(t *testing.T) {
	kcd1 := generateTestingKCD("1", "testing", "version1", "Success")
	kcd2 := generateTestingKCD("2", "testing", "version1", "Success")
	kcd3 := generateTestingKCD("3", "testing", "version1", "Success")
	kcd4 := generateTestingKCD("4", "testing", "version1", "Success")
	kcd5 := generateTestingKCD("5", "testing", "version1", "Success")

	customObjs := []runtime.Object{
		kcd1,
		kcd2,
		kcd3,
		kcd4,
		kcd5,
	}

	k8sObjs := []runtime.Object{}

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

	stopChan := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(1, stopChan); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()

	//Ensure the Add is read first
	time.Sleep(50 * time.Millisecond)
	updateKCD(kcd1, customClient, "Progressing", "version2")
	updateKCD(kcd2, customClient, "Progressing", "version2")
	updateKCD(kcd3, customClient, "Progressing", "version2")
	updateKCD(kcd4, customClient, "Progressing", "version2")
	updateKCD(kcd5, customClient, "Progressing", "version2")

	time.Sleep(50 * time.Millisecond)
	customClient.CustomV1().KCDs("testing").Update(kcd1)

	jobs := make(chan *batchv1.Job, 3)
	jobInformerTest := k8sInformerFactory.Batch().V1().Jobs().Informer()
	jobInformerTest.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			job := obj.(*batchv1.Job)
			t.Logf("job added: %s/%s", job.Namespace, job.Name)
			jobs <- job
		},
	})

	for i := 0; i < 5; i++ {
		select {
		case job := <-jobs:
			t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
			b, _ := json.Marshal(job)
			fmt.Println(string(b))
		case <-time.After(wait.ForeverTestTimeout):
			t.Error("Informer did not get the added pod")
		}
	}
	select {
	case <-jobs:
		t.Error("Too many jobs expected")
	default:
		close(stopChan)
	}
}

func updateKCD(kcd *customv1.KCD, customClient *customclientfake.Clientset, status, version string) {
	kcd.Status.CurrStatus = status
	kcd.Status.CurrVersion = version
	customClient.CustomV1().KCDs("testing").Update(kcd)
}
func TestControllerInitilization(t *testing.T) {

	kcd1 := generateTestingKCD("1", "testing", "version1", "Success")
	kcd2 := generateTestingKCD("2", "testing", "version1", "Failed")
	kcd3 := generateTestingKCD("3", "testing", "version1", "Progressing")

	customObjs := []runtime.Object{
		kcd1,
		kcd2,
		kcd3,
	}

	k8sObjs := []runtime.Object{}

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

	stopChan := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(2, stopChan); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()

	jobs := make(chan *batchv1.Job, 3)
	jobInformerTest := k8sInformerFactory.Batch().V1().Jobs().Informer()
	jobInformerTest.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			job := obj.(*batchv1.Job)
			t.Logf("job added: %s/%s", job.Namespace, job.Name)
			jobs <- job
		},
	})

	select {
	case job := <-jobs:
		t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
		b, _ := json.Marshal(job)
		fmt.Println(string(b))
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Informer did not get the added pod")
	}
}

func TestControllerInitilizationRollback(t *testing.T) {
	kcd1 := generateTestingKCD("1", "testing", "version1", "Progressing")
	job1 := generateTestingJob("1", "testing", "version1", "Complete")
	kcd2 := generateTestingKCD("2", "testing", "version1", "Progressing")
	job2 := generateTestingJob("2", "testing", "version1", "Complete")
	kcd3 := generateTestingKCD("3", "testing", "version1", "Progressing")
	job3 := generateTestingJob("3", "testing", "version1", "Complete")
	kcd4 := generateTestingKCD("4", "testing", "version1", "Progressing")
	job4 := generateTestingJob("4", "testing", "version1", "Complete")
	kcd5 := generateTestingKCD("5", "testing", "version1", "Progressing")
	job5 := generateTestingJob("5", "testing", "version1", "Complete")
	kcd6 := generateTestingKCD("6", "testing", "version1", "Progressing")
	job6 := generateTestingJob("6", "testing", "version1", "Failed")

	customObjs := []runtime.Object{
		kcd1,
		kcd2,
		kcd3,
		kcd4,
		kcd5,
		kcd6,
	}

	k8sObjs := []runtime.Object{
		job1,
		job2,
		job3,
		job4,
		job5,
		job6,
	}

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

	stopCh := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(2, stopCh); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()

	jobs := make(chan *batchv1.Job, 3)
	jobInformerTest := k8sInformerFactory.Batch().V1().Jobs().Informer()
	jobInformerTest.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			job := obj.(*batchv1.Job)
			t.Logf("job added: %s/%s", job.Namespace, job.Name)
			jobs <- job
		},
	})
	cache.WaitForCacheSync(stopCh, jobInformerTest.HasSynced)

	//We should see 2 new jobs added to replace the existing ones
	for i := 0; i < 12; i++ {
		select {
		case job := <-jobs:
			t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
			b, _ := json.Marshal(job)
			fmt.Println(string(b))
		case <-time.After(wait.ForeverTestTimeout):
			t.Error("Informer did not get the added pod")
		}
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

func generateTestingJob(postfix, namespace, version, status string) *batchv1.Job {
	selector := make(map[string]string)
	selector["kcdapp"] = fmt.Sprintf("testapp%s", postfix)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("deploy-tracker-testapp%s-%s", postfix, version),
			Namespace: namespace,
		},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{
				batchv1.JobCondition{
					Type:   batchv1.JobConditionType(status),
					Status: "True",
				},
			},
		},
	}
}
