package resourcetopologyexporter

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/kubeconf"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/notification"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podreadiness"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/sharedcpuspool"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/ratelimiter"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
)

type Args struct {
	Debug                  bool
	ReferenceContainer     *sharedcpuspool.ContainerIdent
	TopologyManagerPolicy  string
	TopologyManagerScope   string
	KubeletConfigFile      string
	KubeletStateDirs       []string
	PodResourcesSocketPath string
	SleepInterval          time.Duration
	PodReadinessEnable     bool
	NotifyFilePath         string
	MaxEventsPerTimeUnit   int64
	TimeUnitToLimitEvents  time.Duration
}

type tmSettings struct {
	config nrtupdater.TMConfig
}

func Execute(cli podresourcesapi.PodResourcesListerClient, nrtupdaterArgs nrtupdater.Args, resourcemonitorArgs resourcemonitor.Args, rteArgs Args) error {
	tmConf, err := getTopologyManagerSettings(rteArgs)
	if err != nil {
		return err
	}

	var condChan chan v1.PodCondition
	if rteArgs.PodReadinessEnable {
		condChan = make(chan v1.PodCondition)
		condIn, err := podreadiness.NewConditionInjector()
		if err != nil {
			return err
		}
		condIn.Run(condChan)
	}

	eventSource, err := createEventSource(&rteArgs)
	if err != nil {
		return err
	}

	resObs, err := NewResourceObserver(cli, resourcemonitorArgs)
	if err != nil {
		return err
	}
	go resObs.Run(eventSource.Events(), condChan)

	upd := nrtupdater.NewNRTUpdater(nrtupdaterArgs, tmConf.config)
	go upd.Run(resObs.Infos, condChan)

	go eventSource.Run()

	eventSource.Wait()  // will never return
	eventSource.Close() // still we try to clean after ourselves :)
	return nil          // unreachable
}

func createEventSource(rteArgs *Args) (notification.EventSource, error) {
	var es notification.EventSource

	eventSource, err := notification.NewUnlimitedEventSource()
	if err != nil {
		return nil, err
	}

	err = eventSource.SetInterval(rteArgs.SleepInterval)
	if err != nil {
		return nil, err
	}

	err = eventSource.AddFile(rteArgs.NotifyFilePath)
	if err != nil {
		return nil, err
	}

	err = eventSource.AddDirs(rteArgs.KubeletStateDirs)
	if err != nil {
		return nil, err
	}

	es = eventSource

	// If rate limit parameters are configured set it up
	if rteArgs.MaxEventsPerTimeUnit > 0 && rteArgs.TimeUnitToLimitEvents > 0 {
		es, err = ratelimiter.NewRateLimitedEventSource(eventSource, uint64(rteArgs.MaxEventsPerTimeUnit), rteArgs.TimeUnitToLimitEvents)
		if err != nil {
			return nil, err
		}
	}

	return es, nil
}

func getTopologyManagerSettings(rteArgs Args) (tmSettings, error) {
	if rteArgs.TopologyManagerPolicy != "" && rteArgs.TopologyManagerScope != "" {
		tmConf := tmSettings{
			config: nrtupdater.TMConfig{
				Policy: rteArgs.TopologyManagerPolicy,
				Scope:  rteArgs.TopologyManagerScope,
			},
		}
		klog.Infof("using given Topology Manager policy %q scope %q", tmConf.config.Policy, tmConf.config.Scope)
		return tmConf, nil
	}
	if rteArgs.KubeletConfigFile != "" {
		klConfig, err := kubeconf.GetKubeletConfigFromLocalFile(rteArgs.KubeletConfigFile)
		if err != nil {
			return tmSettings{}, fmt.Errorf("error getting topology Manager Policy: %w", err)
		}
		tmConf := tmSettings{
			config: nrtupdater.TMConfig{
				Policy: klConfig.TopologyManagerPolicy,
				Scope:  klConfig.TopologyManagerScope,
			},
		}
		klog.Infof("using detected Topology Manager policy %q scope %q", tmConf.config.Policy, tmConf.config.Scope)
		return tmConf, nil
	}
	return tmSettings{}, fmt.Errorf("cannot find the kubelet Topology Manager policy")
}
