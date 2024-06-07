/*
 Copyright 2024 confidentialfilesystems
*/

package driver

import (
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
)

type cfsControllerService struct {
	controllerService
}

func newCfsControllerService(k8sClient *k8sclient.K8sClient) (cfsControllerService, error) {
	controllerService, _ := newControllerService(k8sClient)

	return cfsControllerService{
		controllerService: controllerService,
	}, nil
}
