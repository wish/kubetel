package tracker_test

import (
	"fmt"
	"testing"

	customv1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeployUpdate(t *testing.T) {

}

func TestKCDFinishOnStart(t *testing.T) {

}

func TestKCDFinish(t *testing.T) {

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
