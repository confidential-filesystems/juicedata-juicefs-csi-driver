package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	commonUtil "github.com/confidential-filesystems/csi-driver-common/service/util"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/juicefs"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"github.com/juicedata/juicefs-csi-driver/pkg/webhook/handler/mutate"
)

type CfsSidecarHandler struct {
	Client        *k8sclient.K8sClient
	eventRecorder record.EventRecorder
	// A decoder will be automatically injected
	decoder *admission.Decoder
}

func NewCfsSidecarHandler(client *k8sclient.K8sClient, eventRecorder record.EventRecorder) *CfsSidecarHandler {
	return &CfsSidecarHandler{
		Client:        client,
		eventRecorder: eventRecorder,
	}
}

func (s *CfsSidecarHandler) Handle(ctx context.Context, request admission.Request) admission.Response {
	var (
		err error
		pod = &corev1.Pod{}
	)
	defer func() {
		if err != nil && (pod.Name != "" || pod.GenerateName != "") {
			var eventTarget runtime.Object = pod
			if owner, _ := commonUtil.GetFirstRuntimeObjectOwner(ctx, s.Client, pod); owner != nil {
				eventTarget = owner
			}
			s.eventRecorder.Eventf(eventTarget, corev1.EventTypeWarning, "Injecting", "Failed to inject sidecar container: %s", err)
		}
	}()
	raw := request.Object.Raw
	klog.V(6).Infof("[CfsSidecarHandler] get pod: %s", string(raw))
	err = s.decoder.Decode(request, pod)
	if err != nil {
		klog.Error(err, "unable to decoder pod from req")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// check if pod has done label
	if util.CheckExpectValue(pod.Labels, config.InjectSidecarDone, config.True) {
		klog.Infof("[CfsSidecarHandler] skip mutating the pod because injection is done. Pod %s namespace %s", pod.Name, pod.Namespace)
		return admission.Allowed("skip mutating the pod because injection is done")
	}

	// check if pod has disable label
	if util.CheckExpectValue(pod.Labels, config.InjectSidecarDisable, config.True) {
		klog.Infof("[CfsSidecarHandler] skip mutating the pod because injection is disabled. Pod %s namespace %s", pod.Name, pod.Namespace)
		return admission.Allowed("skip mutating the pod because injection is disabled")
	}

	// check if pod use JuiceFS Volume
	used, pair, err := util.GetVolumes(ctx, s.Client, pod)
	if err != nil {
		klog.Errorf("[CfsSidecarHandler] get pv from pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
		err = nil // disable the event
		return admission.Errored(http.StatusBadRequest, errors.New("could not get pv from pod"))
	} else if !used {
		klog.Infof("[CfsSidecarHandler] skip mutating the pod because it doesn't use JuiceFS Volume. Pod %s namespace %s", pod.Name, pod.Namespace)
		return admission.Allowed("skip mutating the pod because it doesn't use JuiceFS Volume")
	}

	jfs := juicefs.NewJfsProvider(nil, s.Client)
	sidecarMutate := mutate.NewCfsSidecarMutate(s.Client, jfs, pair)
	klog.Infof("[CfsSidecarHandler] start injecting cfs client as sidecar in pod [%s] namespace [%s].", pod.Name, pod.Namespace)
	out, err := sidecarMutate.Mutate(ctx, pod)
	if err != nil {
		err = fmt.Errorf("mutate err: %s", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	pod = out

	marshaledPod, err := json.Marshal(pod)
	if err != nil {
		klog.Error(err, "unable to marshal pod")
		err = errors.New("unable to marshal pod")
		return admission.Errored(http.StatusInternalServerError, err)
	}
	klog.V(6).Infof("[CfsSidecarHandler] mutated pod: %s", string(marshaledPod))
	resp := admission.PatchResponseFromRaw(raw, marshaledPod)
	return resp
}

// InjectDecoder injects the decoder.
func (s *CfsSidecarHandler) InjectDecoder(d *admission.Decoder) error {
	s.decoder = d
	return nil
}
