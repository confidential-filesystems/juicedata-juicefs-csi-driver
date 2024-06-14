/*
 Copyright 2022 Juicedata Inc

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

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"

	commonConfig "github.com/confidential-filesystems/csi-driver-common/config"
	"github.com/juicedata/juicefs-csi-driver/cmd/app"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/driver"
	k8s "github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(corev1.AddToScheme(scheme))
}
func parseControllerConfig() {
	config.ByProcess = process
	config.Webhook = webhook
	config.Provisioner = provisioner
	config.CacheClientConf = cacheConf
	config.FormatInPod = formatInPod
	config.ValidatingWebhook = validationWebhook
	commonConfig.DriverName = config.DriverName
	commonConfig.DriverVersion = "v0.0.1"
	if os.Getenv(commonConfig.DriverName) != "" {
		config.DriverName = os.Getenv(commonConfig.DriverName)
	} else {
		config.DriverName += commonConfig.DefaultProvisionerNameSuffix
	}
	if os.Getenv(commonConfig.EnvWorkloadRuntimeClassName) != "" {
		config.WorkloadRuntimeClassName = os.Getenv(commonConfig.EnvWorkloadRuntimeClassName)
	}
	if os.Getenv(commonConfig.EnvResourceServerUrl) != "" {
		config.ResourceServerUrl = os.Getenv(commonConfig.EnvResourceServerUrl)
	}
	if os.Getenv(commonConfig.EnvFsManagerPort) != "" {
		port, err := strconv.Atoi(os.Getenv(commonConfig.EnvFsManagerPort))
		if err == nil {
			config.FSManagerPort = port
		}
	}
	if os.Getenv(commonConfig.EnvWorkloadInitImage) != "" {
		config.WorkloadInitImage = os.Getenv(commonConfig.EnvWorkloadInitImage)
	}
	if os.Getenv(commonConfig.EnvWorkloadSideCarImage) != "" {
		config.WorkloadSideCarImage = os.Getenv(commonConfig.EnvWorkloadSideCarImage)
	}
	if config.WorkloadSideCarImage == "" {
		klog.Error("no workload sidecar image setting")
		os.Exit(0)
	}
	config.PodName = os.Getenv("POD_NAME")
	if config.PodName == "" {
		klog.Fatalln("Pod name can't be null.")
		os.Exit(0)
	}
	// enable mount manager by default in csi controller
	config.MountManager = true
	if process {
		// if run in process, does not need pod info
		config.FormatInPod = false
		config.MountManager = false
		config.Webhook = false
		config.Provisioner = false
		return
	}
	if webhook {
		// if enable webhook, does not need mount manager & must format in pod
		config.FormatInPod = true
		config.MountManager = false
		config.ByProcess = false
	}
	if jfsImmutable := os.Getenv("JUICEFS_IMMUTABLE"); jfsImmutable != "" {
		// check if running in an immutable environment
		if immutable, err := strconv.ParseBool(jfsImmutable); err == nil {
			config.Immutable = immutable
		} else {
			klog.Errorf("cannot parse JUICEFS_IMMUTABLE: %v", err)
		}
	}

	config.NodeName = os.Getenv("NODE_NAME")
	config.Namespace = os.Getenv("JUICEFS_MOUNT_NAMESPACE")
	config.MountPointPath = os.Getenv("JUICEFS_MOUNT_PATH")
	config.JFSConfigPath = os.Getenv("JUICEFS_CONFIG_PATH")
	config.MountLabels = os.Getenv("JUICEFS_MOUNT_LABELS")

	if mountPodImage := os.Getenv("JUICEFS_CE_MOUNT_IMAGE"); mountPodImage != "" {
		config.CEMountImage = mountPodImage
	}
	if mountPodImage := os.Getenv("JUICEFS_EE_MOUNT_IMAGE"); mountPodImage != "" {
		config.EEMountImage = mountPodImage
	}
	if mountPodImage := os.Getenv("JUICEFS_MOUNT_IMAGE"); mountPodImage != "" {
		// check if it's CE or EE
		hasCE, hasEE := util.ImageResol(mountPodImage)
		if hasCE {
			config.CEMountImage = mountPodImage
		}
		if hasEE {
			config.EEMountImage = mountPodImage
		}
	}

	if !config.Webhook {
		// When not in sidecar mode, we should inherit attributes from CSI Node pod.
		k8sclient, err := k8s.NewClient()
		if err != nil {
			klog.V(5).Infof("Can't get k8s client: %v", err)
			os.Exit(0)
		}
		CSINodeDsName := "juicefs-" + config.CfsName + "-csi-node"
		if name := os.Getenv("JUICEFS_CSI_NODE_DS_NAME"); name != "" {
			CSINodeDsName = name
		}
		ds, err := k8sclient.GetDaemonSet(context.TODO(), CSINodeDsName, config.Namespace)
		if err != nil {
			klog.V(5).Infof("Can't get DaemonSet %s: %v", CSINodeDsName, err)
			os.Exit(0)
		}
		config.CSIPod = corev1.Pod{
			Spec: ds.Spec.Template.Spec,
		}
	}
}

func controllerRun() {
	parseControllerConfig()

	if version {
		info, err := driver.GetVersionJSON()
		if err != nil {
			klog.Fatalln(err)
		}
		fmt.Println(info)
		os.Exit(0)
	}
	if nodeID == "" {
		klog.Fatalln("nodeID must be provided")
	}

	// http server for pprof
	go func() {
		port := 6060
		for {
			http.ListenAndServe(fmt.Sprintf("localhost:%d", port), nil)
			port++
		}
	}()

	registerer, registry := util.NewPrometheus(config.NodeName)
	// http server for metrics
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{
				// Opt into OpenMetrics to support exemplars.
				EnableOpenMetrics: true,
			},
		))
		server := &http.Server{
			Addr:    fmt.Sprintf(":%d", config.WebPort),
			Handler: mux,
		}
		server.ListenAndServe()
	}()

	// enable mount manager in csi controller
	if config.MountManager {
		go func() {
			ctx := ctrl.SetupSignalHandler()
			mgr, err := app.NewMountManager(leaderElection, leaderElectionNamespace, leaderElectionLeaseDuration)
			if err != nil {
				klog.Error(err)
				return
			}
			mgr.Start(ctx)
		}()
	}

	// enable webhook in csi controller
	if config.Webhook {
		go func() {
			ctx := ctrl.SetupSignalHandler()
			mgr, err := app.NewWebhookManager(certDir, webhookPort, leaderElection, leaderElectionNamespace, leaderElectionLeaseDuration)
			if err != nil {
				klog.Fatalln(err)
			}

			if err := mgr.Start(ctx); err != nil {
				klog.Fatalln(err)
			}
		}()
	}

	drv, err := driver.NewCfsDriver(endpoint, nodeID, leaderElection, leaderElectionNamespace, leaderElectionLeaseDuration, registerer)
	if err != nil {
		klog.Fatalln(err)
	}
	if err := drv.Run(); err != nil {
		klog.Fatalln(err)
	}
}
