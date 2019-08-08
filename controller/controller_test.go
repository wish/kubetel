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
	kcd1 := generateTestingKCD("1", "testing", "1111111", "Success") //

	customObjs := []runtime.Object{
		kcd1,
	}

	k8sObjs := []runtime.Object{}

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

	stopController := make(chan struct{})

	stopChan := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(1, stopController); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()

	k8sInformerFactory.Start(stopChan)

	//Ensure the Add is read first
	time.Sleep(50 * time.Millisecond)

	stopCh := make(chan struct{})
	k8sInformerFactory.Start(stopCh)
	kcd1.Status.CurrStatus = "Progressing"
	kcd1.Status.CurrVersion = "2222222"
	customClient.CustomV1().KCDs("testing").Update(kcd1)

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

	go jobInformerTest.Run(stopChan)
	cache.WaitForCacheSync(stopChan, jobInformerTest.HasSynced)

	select {
	case job := <-jobs:
		t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
		b, _ := json.Marshal(job)
		fmt.Println(string(b))
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Informer did not get the added pod")
	}

}

func TestControllerInitilization(t *testing.T) {

	kcd1 := generateTestingKCD("1", "testing", "1111111", "Success")     //Case 1 Finished Deploymnet, No Job to be made
	kcd2 := generateTestingKCD("2", "testing", "2222222", "Failed")      // Case 2  Finished Deploymnet, No job to be made
	kcd3 := generateTestingKCD("3", "testing", "3333333", "Progressing") //Case 3 Propgressing Deplyment (New Version), New Job Expected

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

	stopController := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(2, stopController); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()

	//time.Sleep(5 * time.Second)
	stopChan := make(chan struct{})

	jobs := make(chan *batchv1.Job, 3)
	jobInformerTest := k8sInformerFactory.Batch().V1().Jobs().Informer()
	jobInformerTest.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			job := obj.(*batchv1.Job)
			t.Logf("job added: %s/%s", job.Namespace, job.Name)
			jobs <- job
		},
	})

	k8sInformerFactory.Start(stopChan)
	cache.WaitForCacheSync(stopChan, jobInformerTest.HasSynced)
	select {
	case job := <-jobs:
		t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
		b, _ := json.Marshal(job)
		fmt.Println(string(b))
	case <-time.After(wait.ForeverTestTimeout):
		t.Error("Informer did not get the added pod")
	}

	close(stopChan)
	close(stopController)

}

//Weird Bug Closing the shared job informer
//Then calling create job causes panic in the fake controller sometimes

func TestControllerInitilizationRollback(t *testing.T) {
	kcd4 := generateTestingKCD("4", "testing", "4444444", "Progressing") //Case 4 Propgressing Deplyment (Rollback) , New Job Expected
	job4 := generateTestingJob("4", "testing", "4444444", "Complete")

	kcd5 := generateTestingKCD("5", "testing", "5555555", "Progressing") //Case 5 Propgressing Deplyment (Rollback) , New Job Expected
	job5 := generateTestingJob("5", "testing", "5555555", "Failed")

	customObjs := []runtime.Object{
		kcd4,
		kcd5,
	}

	k8sObjs := []runtime.Object{
		job4,
		job5,
	}

	k8sClient := k8sclientfake.NewSimpleClientset(k8sObjs...)
	customClient := customclientfake.NewSimpleClientset(customObjs...)

	kcdcInformerFactory := informer.NewSharedInformerFactory(customClient, time.Second*30)
	k8sInformerFactory := k8sinformers.NewSharedInformerFactory(k8sClient, time.Second*30)

	stopCh := make(chan struct{})

	c, _ := controller.NewController(k8sClient, customClient, kcdcInformerFactory, k8sInformerFactory)
	go func() {
		if err := c.Run(1, nil); err != nil {
			t.Fatal("Failed to start controller: ", err)
		}
	}()
	k8sInformerFactory.Start(stopCh)

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
	for i := 0; i < 4; i++ {
		select {
		case job := <-jobs:
			t.Logf("Got job from channel: %s/%s", job.Namespace, job.Name)
			b, _ := json.Marshal(job)
			fmt.Println(string(b))
		case <-time.After(wait.ForeverTestTimeout):
			t.Error("Informer did not get the added pod")
		}
	}
	close(stopCh)
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
