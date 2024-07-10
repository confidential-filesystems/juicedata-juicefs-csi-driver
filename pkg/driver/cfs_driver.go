/*
 Copyright 2024 confidentialfilesystems
*/

package driver

import (
	"context"
	commonConfig "github.com/confidential-filesystems/filesystem-csi-driver-common/config"
	"net"
	"time"

	commonDriver "github.com/confidential-filesystems/filesystem-csi-driver-common/service/driver"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

// CfsDriver struct
type CfsDriver struct {
	cfsControllerService
	cfsProvisionerService
	*commonDriver.IdentityServer

	srv      *grpc.Server
	endpoint string
}

func NewCfsDriver(endpoint string, nodeID string,
	leaderElection bool,
	leaderElectionNamespace string,
	leaderElectionLeaseDuration time.Duration, reg prometheus.Registerer) (*CfsDriver, error) {
	klog.Infof("Driver: %v version %v commit %v date %v", config.DriverName, commonConfig.DriverVersion, gitCommit, buildDate)

	k8sClient, err := k8sclient.NewClient()
	if err != nil {
		klog.V(5).Infof("Can't get k8s client: %v", err)
		return nil, err
	}

	cs, err := newCfsControllerService(k8sClient)
	if err != nil {
		return nil, err
	}

	ps, err := newCfsProvisionerService(k8sClient, leaderElection, leaderElectionNamespace, leaderElectionLeaseDuration, reg)
	if err != nil {
		return nil, err
	}

	return &CfsDriver{
		cfsControllerService:  cs,
		cfsProvisionerService: ps,
		IdentityServer:        commonDriver.NewIdentityServer(config.DriverName),
		endpoint:              endpoint,
	}, nil
}

// Run runs the server
func (d *CfsDriver) Run() error {
	go d.cfsProvisionerService.Run(context.Background())
	scheme, addr, err := util.ParseEndpoint(d.endpoint)
	if err != nil {
		return err
	}

	listener, err := net.Listen(scheme, addr)
	if err != nil {
		return err
	}

	logErr := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("GRPC error: %v", err)
		}
		return resp, err
	}
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logErr),
	}
	d.srv = grpc.NewServer(opts...)

	csi.RegisterIdentityServer(d.srv, d)
	csi.RegisterControllerServer(d.srv, d)

	klog.Infof("Listening for connection on address: %#v", listener.Addr())
	return d.srv.Serve(listener)
}

// Stop stops server
func (d *CfsDriver) Stop() {
	klog.Infof("Stopped server")
	d.srv.Stop()
}
