package tracker

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
)

//StatusData format of data to send to kubedeploy
type StatusData struct {
	Cluster   string
	Timestamp time.Time
	Deploy    appsv1.Deployment
	Version   string
	Success   bool
}
