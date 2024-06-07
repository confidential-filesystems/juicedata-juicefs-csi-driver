package k8sclient

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	crGv = "/apis/confidentialfilesystems.com/v1"
	crK  = "cfspecs"
)

type CfSpec struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CfSpecSpec   `json:"spec"`
	Status            CfSpecStatus `json:"status,omitempty"`
}

type CfSpecSpec struct {
	Filesystem Filesystem `json:"filesystem"`
	Metadata   Metadata   `json:"metadata"`
	Storage    Storage    `json:"storage"`
}

type Filesystem struct {
	Name         string `json:"name"`
	Action       string `json:"action"`
	StorageClass string `json:"storageClass"`
}

type Metadata struct {
	PersistentVolumeClaim string `json:"persistentVolumeClaim"`
	Service               string `json:"service"`
}

type Storage struct {
	Type     string `json:"type"`
	Capacity string `json:"capacity"`
	Access   string `json:"access"`
}

type CfSpecPhase string

const (
	CfSpecCreateUnfinished CfSpecPhase = "CreateUnfinished"
	CfSpecCreateFinished   CfSpecPhase = "CreateFinished"
	CfSpecDeleteUnfinished CfSpecPhase = "DeleteUnfinished"
	CfSpecDeleteFinished   CfSpecPhase = "DeleteFinished"
)

type CfSpecStatus struct {
	Phase          CfSpecPhase `json:"phase"`
	Reason         string      `json:"reason,omitempty"`
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
	CreateTime     metav1.Time `json:"createTime"`
}

func (k *K8sClient) GetCr(ctx context.Context, crName string) (*CfSpec, error) {
	klog.V(6).Infof("Get cr %s", crName)
	result := k.CoreV1().RESTClient().Get().AbsPath(crGv).Resource(crK).Name(crName).Do(ctx)
	err := result.Error()
	if err != nil {
		klog.V(5).Infof("Can't get Cr %s: %v", crName, err)
		return nil, err
	}
	raw, err := result.Raw()
	if err != nil {
		klog.V(5).Infof("Can't get Cr %s: %v", crName, err)
		return nil, err
	}
	cr := &CfSpec{}
	err = json.Unmarshal(raw, cr)
	if err != nil {
		klog.V(5).Infof("Can't unmarshal Cr %s: %v", crName, err)
		return nil, err
	}

	return cr, nil
}
