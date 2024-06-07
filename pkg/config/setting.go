/*
Copyright 2021 Juicedata Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog"

	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util/security"
)

const (
	podInfoName      = "csi.storage.k8s.io/pod.name"
	podInfoNamespace = "csi.storage.k8s.io/pod.namespace"
)

type JfsSetting struct {
	IsCe   bool
	UsePod bool

	UUID               string
	Name               string               `json:"name"`
	MetaUrl            string               `json:"metaurl"`
	Source             string               `json:"source"`
	Storage            string               `json:"storage"`
	FormatOptions      string               `json:"format-options"`
	CachePVCs          []CachePVC           // PVC using by mount pod
	CacheEmptyDir      *CacheEmptyDir       // EmptyDir using by mount pod
	CacheInlineVolumes []*CacheInlineVolume // InlineVolume using by mount pod
	CacheDirs          []string             // hostPath using by mount pod
	ClientConfPath     string               `json:"-"`

	// put in secret
	SecretKey     string            `json:"secret-key,omitempty"`
	SecretKey2    string            `json:"secret-key2,omitempty"`
	Token         string            `json:"token,omitempty"`
	Passphrase    string            `json:"passphrase,omitempty"`
	Envs          map[string]string `json:"envs_map,omitempty"`
	EncryptRsaKey string            `json:"encrypt_rsa_key,omitempty"`
	InitConfig    string            `json:"initconfig,omitempty"`
	Configs       map[string]string `json:"configs_map,omitempty"`

	// put in volCtx
	MountPodLabels      map[string]string `json:"mount_pod_labels"`
	MountPodAnnotations map[string]string `json:"mount_pod_annotations"`
	DeletedDelay        string            `json:"deleted_delay"`
	CleanCache          bool              `json:"clean_cache"`
	HostPath            []string          `json:"host_path"`
	ServiceAccountName  string
	Resources           corev1.ResourceRequirements

	// mount
	VolumeId   string   // volumeHandle of PV
	UniqueId   string   // mount pod name is generated by uniqueId
	MountPath  string   // mountPath of mount pod or process mount
	TargetPath string   // which bind to container path
	Options    []string // mount options
	FormatCmd  string   // format or auth
	SubPath    string   // subPath which is to be created or deleted
	SecretName string   // secret with JuiceFS volume credentials

	Attr PodAttr
}

type PodAttr struct {
	Namespace            string
	MountPointPath       string
	JFSConfigPath        string
	JFSMountPriorityName string

	// inherit from csi
	Image            string
	HostNetwork      bool
	HostAliases      []corev1.HostAlias
	HostPID          bool
	HostIPC          bool
	DNSConfig        *corev1.PodDNSConfig
	DNSPolicy        corev1.DNSPolicy
	ImagePullSecrets []corev1.LocalObjectReference
	PreemptionPolicy *corev1.PreemptionPolicy
	Tolerations      []corev1.Toleration
}

// info of app pod
type AppInfo struct {
	Name      string
	Namespace string
}

type CachePVC struct {
	PVCName string
	Path    string
}

type CacheEmptyDir struct {
	Medium    string
	SizeLimit resource.Quantity
	Path      string
}

type CacheInlineVolume struct {
	CSI  *corev1.CSIVolumeSource
	Path string
}

func ParseSetting(secrets, volCtx map[string]string, options []string, usePod bool) (*JfsSetting, error) {
	jfsSetting := JfsSetting{
		Options: []string{},
	}
	if options != nil {
		jfsSetting.Options = options
	}
	var err error
	if secrets == nil {
		jfsSetting.IsCe = true // cfs no secret, always as ce
		//return &jfsSetting, nil
	} else {
		secretStr, err := json.Marshal(secrets)
		if err != nil {
			return nil, err
		}
		if err := parseYamlOrJson(string(secretStr), &jfsSetting); err != nil {
			return nil, err
		}

		if secrets["name"] == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Empty name")
		}
		jfsSetting.Name = secrets["name"]
		jfsSetting.Storage = secrets["storage"]
	}
	jfsSetting.Envs = make(map[string]string)
	jfsSetting.Configs = make(map[string]string)
	jfsSetting.ClientConfPath = DefaultClientConfPath
	jfsSetting.CacheDirs = []string{}
	jfsSetting.CachePVCs = []CachePVC{}

	// parse pvc of cache
	dirs := []string{}
	if volCtx != nil && volCtx[cachePVC] != "" {
		cachePVCs := strings.Split(strings.TrimSpace(volCtx[cachePVC]), ",")
		for i, pvc := range cachePVCs {
			if pvc == "" {
				continue
			}
			volPath := fmt.Sprintf("/var/jfsCache-%d", i)
			jfsSetting.CachePVCs = append(jfsSetting.CachePVCs, CachePVC{
				PVCName: pvc,
				Path:    volPath,
			})
			dirs = append(dirs, volPath)
		}
	}
	// parse emptydir of cache
	if volCtx != nil {
		if _, ok := volCtx[cacheEmptyDir]; ok {
			volPath := "/var/jfsCache-emptyDir"
			dirs = append(dirs, volPath)
			cacheEmptyDirs := strings.Split(strings.TrimSpace(volCtx[cacheEmptyDir]), ":")
			var (
				medium    string
				sizeLimit string
			)
			if len(cacheEmptyDirs) == 1 {
				medium = strings.TrimSpace(cacheEmptyDirs[0])
			}
			if len(cacheEmptyDirs) == 2 {
				medium = strings.TrimSpace(cacheEmptyDirs[0])
				sizeLimit = strings.TrimSpace(cacheEmptyDirs[1])
			}
			jfsSetting.CacheEmptyDir = &CacheEmptyDir{
				Medium: medium,
				Path:   volPath,
			}
			klog.Infof("sizeLimit of emptyDir is %s", sizeLimit)
			if sizeLimit != "" {
				if jfsSetting.CacheEmptyDir.SizeLimit, err = resource.ParseQuantity(sizeLimit); err != nil {
					return nil, err
				}
			}
		}
	}
	// parse inline volume of cache
	if volCtx != nil {
		if _, ok := volCtx[cacheInlineVolume]; ok {
			inlineVolumes := []*corev1.CSIVolumeSource{}
			err = json.Unmarshal([]byte(volCtx[cacheInlineVolume]), &inlineVolumes)
			if err != nil {
				return nil, fmt.Errorf("parse cache inline volume error: %v", err)
			}
			jfsSetting.CacheInlineVolumes = make([]*CacheInlineVolume, 0)
			klog.V(6).Infof("get cache inline volume: %v", inlineVolumes)

			for i, inlineVolume := range inlineVolumes {
				volPath := fmt.Sprintf("/var/jfsCache-inlineVolume-%d", i)
				dirs = append(dirs, volPath)
				jfsSetting.CacheInlineVolumes = append(jfsSetting.CacheInlineVolumes, &CacheInlineVolume{
					CSI:  inlineVolume,
					Path: volPath,
				})
			}
		}
	}

	// parse cacheDir in option
	var cacheDirs []string
	for i, o := range options {
		if strings.HasPrefix(o, "cache-dir") {
			optValPair := strings.Split(o, "=")
			if len(optValPair) != 2 {
				continue
			}
			cacheDirs = strings.Split(strings.TrimSpace(optValPair[1]), ":")
			dirs = append(dirs, cacheDirs...)
			options = append(options[:i], options[i+1:]...)
			break
		}
	}

	cacheDir := strings.Join(dirs, ":")
	if cacheDir != "" {
		// replace cacheDir in option
		options = append(options, fmt.Sprintf("cache-dir=%s", cacheDir))
		jfsSetting.Options = options
	}

	if len(dirs) == 0 {
		// set default cache dir
		cacheDirs = []string{"/var/jfsCache"}
	}
	for _, d := range cacheDirs {
		if d != "memory" {
			// filter out "memory"
			jfsSetting.CacheDirs = append(jfsSetting.CacheDirs, d)
		}
	}

	jfsSetting.UsePod = usePod
	jfsSetting.Source = jfsSetting.Name
	if source, ok := secrets["metaurl"]; ok {
		jfsSetting.MetaUrl = source
		jfsSetting.IsCe = ok
		// Default use redis:// scheme
		if !strings.Contains(source, "://") {
			source = "redis://" + source
		}
		jfsSetting.Source = source
	}

	if secrets["secretkey"] != "" {
		jfsSetting.SecretKey = secrets["secretkey"]
	}
	if secrets["secretkey2"] != "" {
		jfsSetting.SecretKey2 = secrets["secretkey2"]
	}

	if secrets["configs"] != "" {
		configStr := secrets["configs"]
		configs := make(map[string]string)
		klog.V(6).Infof("Get configs in secret: %v", configStr)
		if err := parseYamlOrJson(configStr, &configs); err != nil {
			return nil, err
		}
		jfsSetting.Configs = configs
	}

	if secrets["envs"] != "" {
		envStr := secrets["envs"]
		env := make(map[string]string)
		klog.V(6).Infof("Get envs in secret: %v", envStr)
		if err := parseYamlOrJson(envStr, &env); err != nil {
			return nil, err
		}
		jfsSetting.Envs = env
	}

	labels := make(map[string]string)
	if MountLabels != "" {
		klog.V(6).Infof("Get MountLabels from csi env: %v", MountLabels)
		if err := parseYamlOrJson(MountLabels, &labels); err != nil {
			return nil, err
		}
	}

	jfsSetting.ServiceAccountName = CSIPod.Spec.ServiceAccountName

	var preemptionPolicy = CSIPod.Spec.PreemptionPolicy
	if JFSMountPreemptionPolicy != "" {
		policy := corev1.PreemptionPolicy(JFSMountPreemptionPolicy)
		preemptionPolicy = &policy
	}
	// inherit attr from csi
	jfsSetting.Attr = PodAttr{
		Namespace:            Namespace,
		MountPointPath:       MountPointPath,
		JFSConfigPath:        JFSConfigPath,
		JFSMountPriorityName: JFSMountPriorityName,
		HostNetwork:          CSIPod.Spec.HostNetwork,
		HostAliases:          CSIPod.Spec.HostAliases,
		HostPID:              CSIPod.Spec.HostPID,
		HostIPC:              CSIPod.Spec.HostIPC,
		DNSConfig:            CSIPod.Spec.DNSConfig,
		DNSPolicy:            CSIPod.Spec.DNSPolicy,
		ImagePullSecrets:     CSIPod.Spec.ImagePullSecrets,
		PreemptionPolicy:     preemptionPolicy,
		Tolerations:          CSIPod.Spec.Tolerations,
	}
	if jfsSetting.IsCe {
		jfsSetting.Attr.Image = CEMountImage
	} else {
		jfsSetting.Attr.Image = EEMountImage
	}

	if volCtx != nil && volCtx[mountPodImageKey] != "" {
		jfsSetting.Attr.Image = volCtx[mountPodImageKey]
	}

	// set default resource limit & request
	jfsSetting.Resources = getDefaultResource()

	if volCtx != nil {
		// subPath
		if volCtx["subPath"] != "" {
			jfsSetting.SubPath = volCtx["subPath"]
		}

		cpuLimit := volCtx[MountPodCpuLimitKey]
		memoryLimit := volCtx[MountPodMemLimitKey]
		cpuRequest := volCtx[MountPodCpuRequestKey]
		memoryRequest := volCtx[MountPodMemRequestKey]
		jfsSetting.Resources, err = ParsePodResources(cpuLimit, memoryLimit, cpuRequest, memoryRequest)
		if err != nil {
			klog.Errorf("Parse resource error: %v", err)
			return nil, err
		}

		if volCtx[mountPodServiceAccount] != "" {
			jfsSetting.ServiceAccountName = volCtx[mountPodServiceAccount]
		}
		if volCtx[cleanCache] == "true" {
			jfsSetting.CleanCache = true
		}
		delay := volCtx[deleteDelay]
		if delay != "" {
			if _, err := time.ParseDuration(delay); err != nil {
				return nil, fmt.Errorf("can't parse delay time %s", delay)
			}
			jfsSetting.DeletedDelay = delay
		}

		labelString := volCtx[mountPodLabelKey]
		annotationSting := volCtx[mountPodAnnotationKey]
		ctxLabel := make(map[string]string)
		if labelString != "" {
			if err := parseYamlOrJson(labelString, &ctxLabel); err != nil {
				return nil, err
			}
		}
		for k, v := range ctxLabel {
			labels[k] = v
		}
		if annotationSting != "" {
			annos := make(map[string]string)
			if err := parseYamlOrJson(annotationSting, &annos); err != nil {
				return nil, err
			}
			jfsSetting.MountPodAnnotations = annos
		}

		var hostPaths []string
		if volCtx[mountPodHostPath] != "" {
			for _, v := range strings.Split(volCtx[mountPodHostPath], ",") {
				p := strings.TrimSpace(v)
				if p != "" {
					hostPaths = append(hostPaths, strings.TrimSpace(v))
				}
			}
			jfsSetting.HostPath = hostPaths
		}
	}
	if len(labels) != 0 {
		jfsSetting.MountPodLabels = labels
	}
	return &jfsSetting, nil
}

func ParseAppInfo(volCtx map[string]string) (*AppInfo, error) {
	// check kubelet access. If not, should turn `podInfoOnMount` on in csiDriver, and fallback to apiServer
	if !ByProcess && !Webhook && KubeletPort != "" && HostIp != "" {
		port, err := strconv.Atoi(KubeletPort)
		if err != nil {
			return nil, err
		}
		kc, err := k8sclient.NewKubeletClient(HostIp, port)
		if err != nil {
			return nil, err
		}
		if _, err := kc.GetNodeRunningPods(); err != nil {
			if volCtx == nil || volCtx[podInfoName] == "" {
				return nil, fmt.Errorf("can not connect to kubelet, please turn `podInfoOnMount` on in csiDriver, and fallback to apiServer")
			}
		}
	}
	if volCtx != nil {
		return &AppInfo{
			Name:      volCtx[podInfoName],
			Namespace: volCtx[podInfoNamespace],
		}, nil
	}
	return nil, nil
}

func (s *JfsSetting) ParseFormatOptions() ([][]string, error) {
	options := strings.Split(s.FormatOptions, ",")
	parsedFormatOptions := make([][]string, 0, len(options))
	for _, option := range options {
		pair := strings.SplitN(strings.TrimSpace(option), "=", 2)
		if len(pair) == 2 && pair[1] == "" {
			return nil, fmt.Errorf("invalid format options: %s", s.FormatOptions)
		}
		key := strings.TrimSpace(pair[0])
		if key == "" {
			// ignore empty key
			continue
		}
		var value string
		if len(pair) == 1 {
			// single key
			value = ""
		} else {
			value = strings.TrimSpace(pair[1])
		}
		parsedFormatOptions = append(parsedFormatOptions, []string{key, value})
	}
	return parsedFormatOptions, nil
}

func (s *JfsSetting) RepresentFormatOptions(parsedOptions [][]string) []string {
	options := make([]string, 0)
	for _, pair := range parsedOptions {
		option := security.EscapeBashStr(pair[0])
		if pair[1] != "" {
			option = fmt.Sprintf("%s=%s", option, security.EscapeBashStr(pair[1]))
		}
		options = append(options, "--"+option)
	}
	return options
}

func (s *JfsSetting) StripFormatOptions(parsedOptions [][]string, strippedKeys []string) []string {
	options := make([]string, 0)
	strippedMap := make(map[string]bool)
	for _, key := range strippedKeys {
		strippedMap[key] = true
	}

	for _, pair := range parsedOptions {
		option := security.EscapeBashStr(pair[0])
		if pair[1] != "" {
			if strippedMap[pair[0]] {
				option = fmt.Sprintf("%s=${%s}", option, pair[0])
			} else {
				option = fmt.Sprintf("%s=%s", option, security.EscapeBashStr(pair[1]))
			}
		}
		options = append(options, "--"+option)
	}
	return options
}

func parseYamlOrJson(source string, dst interface{}) error {
	if err := yaml.Unmarshal([]byte(source), &dst); err != nil {
		if err := json.Unmarshal([]byte(source), &dst); err != nil {
			return status.Errorf(codes.InvalidArgument,
				"Parse yaml or json error: %v", err)
		}
	}
	return nil
}

func ParseYamlOrJson(source string, dst interface{}) error {
	return parseYamlOrJson(source, dst)
}

func ParsePodResources(cpuLimit, memoryLimit, cpuRequest, memoryRequest string) (corev1.ResourceRequirements, error) {
	podLimit := map[corev1.ResourceName]resource.Quantity{}
	podRequest := map[corev1.ResourceName]resource.Quantity{}
	// set default value
	podLimit[corev1.ResourceCPU] = resource.MustParse(DefaultMountPodCpuLimit)
	podLimit[corev1.ResourceMemory] = resource.MustParse(DefaultMountPodMemLimit)
	podRequest[corev1.ResourceCPU] = resource.MustParse(DefaultMountPodCpuRequest)
	podRequest[corev1.ResourceMemory] = resource.MustParse(DefaultMountPodMemRequest)
	var err error
	if cpuLimit != "" {
		if podLimit[corev1.ResourceCPU], err = resource.ParseQuantity(cpuLimit); err != nil {
			return corev1.ResourceRequirements{}, err
		}
		q := podLimit[corev1.ResourceCPU]
		if res := q.Cmp(*resource.NewQuantity(0, resource.DecimalSI)); res <= 0 {
			delete(podLimit, corev1.ResourceCPU)
		}
	}
	if memoryLimit != "" {
		if podLimit[corev1.ResourceMemory], err = resource.ParseQuantity(memoryLimit); err != nil {
			return corev1.ResourceRequirements{}, err
		}
		q := podLimit[corev1.ResourceMemory]
		if res := q.Cmp(*resource.NewQuantity(0, resource.DecimalSI)); res <= 0 {
			delete(podLimit, corev1.ResourceMemory)
		}
	}
	if cpuRequest != "" {
		if podRequest[corev1.ResourceCPU], err = resource.ParseQuantity(cpuRequest); err != nil {
			return corev1.ResourceRequirements{}, err
		}
		q := podRequest[corev1.ResourceCPU]
		if res := q.Cmp(*resource.NewQuantity(0, resource.DecimalSI)); res <= 0 {
			delete(podRequest, corev1.ResourceCPU)
		}
	}
	if memoryRequest != "" {
		if podRequest[corev1.ResourceMemory], err = resource.ParseQuantity(memoryRequest); err != nil {
			return corev1.ResourceRequirements{}, err
		}
		q := podRequest[corev1.ResourceMemory]
		if res := q.Cmp(*resource.NewQuantity(0, resource.DecimalSI)); res <= 0 {
			delete(podRequest, corev1.ResourceMemory)
		}
	}
	return corev1.ResourceRequirements{
		Limits:   podLimit,
		Requests: podRequest,
	}, nil
}

func getDefaultResource() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(DefaultMountPodCpuLimit),
			corev1.ResourceMemory: resource.MustParse(DefaultMountPodMemLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(DefaultMountPodCpuRequest),
			corev1.ResourceMemory: resource.MustParse(DefaultMountPodMemRequest),
		},
	}
}
