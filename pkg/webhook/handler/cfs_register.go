package handler

import (
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
)

const (
	CfsSidecarPath = "/cfs/inject-v1-pod"
)

// CfsRegister registers the handlers to the manager
func CfsRegister(mgr manager.Manager, client *k8sclient.K8sClient) {
	server := mgr.GetWebhookServer()
	server.Register(CfsSidecarPath, &webhook.Admission{Handler: NewCfsSidecarHandler(client)})
	klog.Infof("Registered webhook handler path %s for sidecar", CfsSidecarPath)
}
