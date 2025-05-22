/*
Copyright 2023 The Kubernetes Authors.

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

package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/go-logr/logr"

	ctrlmetricssrv "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/metrics"
)

type Environ struct {
	metrics.Environ
	Mode string
	Conf Config
}

const (
	PortDefault    = 2112
	AddressDefault = "0.0.0.0"

	TLSRootDir = "/etc/secrets/rte"

	TLSCert = "tls.crt"
	TLSKey  = "tls.key"
)

const (
	ServingDefault  = ServingDisabled
	ServingDisabled = "disabled"
	ServingHTTP     = "http" // plaintext
	ServingHTTPTLS  = "httptls"
)

func GetAllServingModes() []string {
	return []string{
		ServingDisabled,
		ServingHTTP,
		ServingHTTPTLS,
	}
}

func IsValidServingMode(mode string) bool {
	return slices.Contains(GetAllServingModes(), mode)
}

func NewDefaultTLSConfig() TLSConfig {
	return TLSConfig{
		CertsDir: TLSRootDir,
		CertFile: TLSCert,
		KeyFile:  TLSKey,
	}
}

type TLSConfig struct {
	CertsDir    string `json:"certsDir,omitempty"`
	CertFile    string `json:"certFile,omitempty"`
	KeyFile     string `json:"keyFile,omitempty"`
	WantCliAuth bool   `json:"wantCliAuth,omitempty"`
}

type Config struct {
	IP   string
	Port int
	TLS  TLSConfig
}

func NewConfig(ip string, port int, tlsConf TLSConfig) Config {
	return Config{
		IP:   ip,
		Port: port,
		TLS:  tlsConf,
	}
}

func NewDefaultConfig() Config {
	return NewConfig(AddressDefault, PortDefault, NewDefaultTLSConfig())
}

func (conf TLSConfig) Clone() TLSConfig {
	return TLSConfig{
		CertsDir: conf.CertsDir,
		CertFile: conf.CertFile,
		KeyFile:  conf.KeyFile,
	}
}

func (conf Config) Validate() error {
	if conf.Port <= 0 {
		return fmt.Errorf("invalid port: %d", conf.Port)
	}
	return nil
}

func (conf Config) BindAddress() string {
	return fmt.Sprintf("%s:%d", conf.IP, conf.Port)
}

func ServingModeIsSupported(value string) (string, error) {
	val := strings.ToLower(value)
	switch val {
	case ServingDisabled:
		return val, nil
	case ServingHTTP:
		return val, nil
	case ServingHTTPTLS:
		return val, nil
	default:
		return val, fmt.Errorf("unsupported method  %q", value)
	}
}

func ServingModeSupported() string {
	return strings.Join(GetAllServingModes(), ",")
}

func PortFromEnv() int {
	envValue, ok := os.LookupEnv("METRICS_PORT")
	if !ok {
		return 0
	}
	port, err := strconv.Atoi(envValue)
	if err != nil {
		return 0
	}
	return port
}

func AddressFromEnv() string {
	ip, ok := os.LookupEnv("METRICS_ADDRESS")
	if !ok {
		return ""
	}
	return ip
}

func Setup(mode string, conf Config) error {
	return SetupWithEnviron(Environ{
		Environ: metrics.Environ{
			Log: logr.Discard(),
		},
		Mode: mode,
		Conf: conf,
	})
}

func SetupWithEnviron(env Environ) error {
	if env.Mode == ServingDisabled {
		env.Log.Info("metrics endpoint disabled")
		return nil
	}

	if err := env.Conf.Validate(); err != nil {
		return err
	}

	var secureServing bool
	switch env.Mode {
	case ServingHTTP:
		secureServing = false
	case ServingHTTPTLS:
		secureServing = true
	default:
		return fmt.Errorf("unknown mode: %v", env.Mode)
	}

	opts := ctrlmetricssrv.Options{
		SecureServing: secureServing,
		BindAddress:   env.Conf.BindAddress(),
		CertDir:       env.Conf.TLS.CertsDir,
		CertName:      env.Conf.TLS.CertFile,
		KeyName:       env.Conf.TLS.KeyFile,
		TLSOpts: []func(*tls.Config){
			WithClientAuthLogger(env.Log, env.Conf.TLS.WantCliAuth),
		},
	}
	srv, err := ctrlmetricssrv.NewServer(opts, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to build server with port %d: %w", env.Conf.Port, err)
	}

	ctx := context.Background()

	go func() {
		err := srv.Start(ctx)
		if err != nil {
			env.Log.Error(err, "error starting the controller-runtime metrics server", "config", env.Conf, "options", opts)
		}
	}()

	return nil
}

func WithClientAuth(cliAuth bool) func(tlscfg *tls.Config) {
	return WithClientAuthLogger(logr.Discard(), cliAuth)
}

func WithClientAuthLogger(lh logr.Logger, cliAuth bool) func(tlscfg *tls.Config) {
	return func(tlscfg *tls.Config) {
		if !cliAuth {
			tlscfg.ClientAuth = tls.NoClientCert
			lh.Info("metrics server configuration", "client authentication", "disabled")
			return
		}
		tlscfg.ClientAuth = tls.RequireAndVerifyClientCert
		lh.Info("metrics server configuration", "client authentication", "enabled")
	}
}
