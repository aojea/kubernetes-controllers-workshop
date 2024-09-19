package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: controller [options]\n\n")
		flag.PrintDefaults()
	}
}

func main() {
	// Try to get the Kubeconfig from flags

	// Parse command line flags and arguments
	flag.Parse()
	flag.VisitAll(func(flag *flag.Flag) {
		log.Printf("FLAG: --%s=%q", flag.Name, flag.Value)
	})

	var err error
	var config *rest.Config
	// Try to get the internal configuration based on the Service Account
	if kubeconfig == "" {
		// If the Pod runs inside a cluster it can use the InCluster configuration
		// creates the in-cluster config
		// 		tokenFile  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		//    rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		// and the apiserver URL
		// host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	} else {
		// use the current context in kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		// List all Pods in the cluster
		pods, err := clientset.CoreV1().Pods(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

		for _, pod := range pods.Items {
			fmt.Printf("Found Pod %s on Namespace %s\n", pod.Name, pod.Namespace)
		}
		fmt.Println("---")
		time.Sleep(10 * time.Second)
	}
}
