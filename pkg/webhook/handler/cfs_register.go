package handler

import (
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	commonConfig "github.com/confidential-filesystems/csi-driver-common/config"
	commonUtil "github.com/confidential-filesystems/csi-driver-common/service/util"
	commonWebhook "github.com/confidential-filesystems/csi-driver-common/service/webhook/pvc"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
)

const (
	CfsSidecarPath       = "/cfs/inject-v1-pod"
	CfsPvcCredentialPath = "/cfs/auth-v1-pvc"
)

// CfsRegister registers the handlers to the manager
func CfsRegister(mgr manager.Manager, client *k8sclient.K8sClient) {
	server := mgr.GetWebhookServer()
	eventRecorder := commonUtil.NewEventRecorder(client, config.PodName, "")
	server.Register(CfsSidecarPath, &webhook.Admission{Handler: NewCfsSidecarHandler(client, eventRecorder)})
	klog.Infof("Registered webhook handler path %s for sidecar", CfsSidecarPath)
	webhookConf := &commonConfig.WebhookConfig{
		InjectConfig: commonConfig.InjectConfig{
			ProvisionerName: config.DriverName,
		},
	}
	serviceConf := &commonConfig.ServiceConfig{
		ResourceServerUrl: config.ResourceServerUrl,
	}
	server.Register(CfsPvcCredentialPath, commonWebhook.NewPvcCredentialHandler(webhookConf, serviceConf, client, eventRecorder, nil))
	klog.Infof("Registered webhook handler path %s for pvc credential", CfsPvcCredentialPath)
}
