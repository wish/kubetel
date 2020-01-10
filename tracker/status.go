package tracker

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
)

//DeployMessage hold a message information
type DeployMessage struct {
	Type    string
	Version string
	Body    interface{}
	Retries int
}

type PodInfo struct {
	Name string
	Status string
	HostIp string
	PodIp string
}

//StatusData format of data to send to kubedeploy
type StatusData struct {
	Cluster   string
	Timestamp time.Time
	Deploy    appsv1.Deployment
	PodInfoList  []PodInfo
	Version   string
}

//FinishedStatusData format for complete job
type FinishedStatusData struct {
	Cluster   string
	Timestamp time.Time
	Deploy    appsv1.Deployment
	Version   string
	Success   bool
}

//FailedPodLogData is FailedPodLogData
type FailedPodLogData struct {
	Cluster   string
	Timestamp time.Time
	Deploy    appsv1.Deployment
	Version   string

	PodName           string
	ContainerName     string
	Logs              string
	PodFailureReason  string
	PodFailureMessage string
}
