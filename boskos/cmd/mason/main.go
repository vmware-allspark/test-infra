// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/boskos/client"
	"sigs.k8s.io/boskos/crds"
	"sigs.k8s.io/boskos/mason"
	"sigs.k8s.io/boskos/ranch"

	"istio.io/test-infra/boskos/gcp"
)

const (
	defaultCleanerCount      = 15
	defaultBoskosRetryPeriod = 15 * time.Second
	defaultBoskosSyncPeriod  = 10 * time.Minute
	defaultOwner             = "mason"
)

var (
	boskosURL         = flag.String("boskos-url", "http://boskos", "Boskos Server URL")
	cleanerCount      = flag.Int("cleaner-count", defaultCleanerCount, "Number of threads running cleanup")
	namespace         = flag.String("namespace", corev1.NamespaceDefault, "Kubernetes namespace to query")
	serviceAccount    = flag.String("service-account", "", "Path to projects service account")
	kubeClientOptions crds.KubernetesClientOptions
)

func main() {
	kubeClientOptions.AddFlags(flag.CommandLine)
	flag.Parse()
	if err := kubeClientOptions.Validate(); err != nil {
		logrus.WithError(err).Fatal("Bad kube client options")
	}
	logrus.SetFormatter(&logrus.JSONFormatter{})
	if *serviceAccount != "" {
		if err := gcp.ActivateServiceAccount(*serviceAccount); err != nil {
			logrus.WithError(err).Fatal("cannot activate service account")
		}
	}

	client, err := client.NewClient(defaultOwner, *boskosURL, "", "")
	if err != nil {
		logrus.WithError(err).Fatal("unable to create boskos client")
	}
	gcpClient, err := gcp.NewClient(*serviceAccount)
	if err != nil {
		logrus.WithError(err).Fatal("unable to create gcp client")
	}
	gcp.SetClient(gcpClient)

	kubeClient, err := kubeClientOptions.CacheBackedClient(*namespace, &crds.DRLCObject{})
	if err != nil {
		logrus.WithError(err).Fatal("unable to get kubernetes client")
	}
	st := ranch.NewStorage(context.Background(), kubeClient, *namespace)

	mason := mason.NewMason(*cleanerCount, client, defaultBoskosRetryPeriod, defaultBoskosSyncPeriod, st)

	// Registering Masonable Converters
	if err := mason.RegisterConfigConverter(gcp.ResourceConfigType, gcp.ConfigConverter); err != nil {
		logrus.WithError(err).Fatalf("unable tp register config converter")
	}

	mason.Start()
	defer mason.Stop()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}
