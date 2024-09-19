package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
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
	// enable logging
	klog.InitFlags(nil)

	// Try to get the Kubeconfig from flags

	// Parse command line flags and arguments
	flag.Parse()
	flag.VisitAll(func(flag *flag.Flag) {
		klog.Infof("FLAG: --%s=%q", flag.Name, flag.Value)
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

	// trap Ctrl+C and call cancel on the context
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// Enable signal handler
	signalCh := make(chan os.Signal, 2)
	defer func() {
		close(signalCh)
		cancel()
	}()
	signal.Notify(signalCh, os.Interrupt, unix.SIGINT)

	go func() {
		select {
		case <-signalCh:
			klog.Infof("Exiting: received signal")
			cancel()
		case <-ctx.Done():
		}
	}()

	informersFactory := informers.NewSharedInformerFactory(clientset, 0)
	podInformer := informersFactory.Core().V1().Pods()
	controller := NewController(clientset, podInformer)
	informersFactory.Start(ctx.Done())

	go controller.Run(ctx, 5) // use 5 workers

	klog.Infoln("---")
	<-ctx.Done()
}

// Controller demonstrates how to implement a controller with client-go.
type Controller struct {
	client           clientset.Interface
	queue            workqueue.TypedRateLimitingInterface[string]
	podLister        corelisters.PodLister
	podsSynced       cache.InformerSynced
	workerLoopPeriod time.Duration
}

// NewController creates a new Controller.
func NewController(client clientset.Interface, podInformer coreinformers.PodInformer) *Controller {
	c := &Controller{
		client:     client,
		podLister:  podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: "mycontroller",
			},
		),
		workerLoopPeriod: time.Second,
	}
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Infof("Pod %s added", key)
				c.queue.Add(key)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(cur)
			if err == nil {
				klog.Infof("Pod %s updated", key)
				c.queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Infof("Pod %s deleted", key)
				c.queue.Add(key)
			}
		},
	})
	return c
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	// Tell the queue that we are done with processing this key. This unblocks the key for other workers
	// This allows safe parallel processing because two pods with the same key are never processed in
	// parallel.
	defer c.queue.Done(key)

	// Invoke the method containing the business logic
	err := c.reconcile(key)
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, key)
	return true
}

func (c *Controller) reconcile(key string) error {
	klog.Infof("processing Pod %s", key)
	start := time.Now()
	defer func() {
		klog.Infof("finished to process Pod %s, it took %v", key, time.Since(start))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Infof("Failed to split meta namespace cache key %s", key)
		return err
	}
	pod, err := c.podLister.Pods(ns).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// This means the pod no longer exist in the informer cache
			// the cache is the eventual consistent state of the cluster
			// so all the reconcilation related to the pod deletion goes here
			// ... do delete stuff ...
			klog.Infof("Pod %s does not exist anymore\n", key)
			return nil
		}
		klog.Errorf("Fetching object with key %s from store failed with %v", key, err)
		return err
	}

	// If the DeletionTimestamp is set means the pod is being deleted
	// This is useful for operations that require to clean up additional resources or perform
	// async operations before our resource is deleted. We use finalizer to avoid the object
	// to be deleted.
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/
	if pod.DeletionTimestamp != nil {
		klog.Infof("Pod %s is being deleted\n", key)
		return nil
	}

	// Note that you also have to check the uid if you have a local controlled resource, which
	// is dependent on the actual instance, to detect that a Pod was recreated with the same name
	klog.Infof("Sync/Add/Update for Pod %s\n", key)
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *Controller) handleErr(err error, key string) {
	if err == nil {
		// Forget about the #AddRateLimited history of the key on every successful synchronization.
		// This ensures that future processing of updates for this key is not delayed because of
		// an outdated error history.
		c.queue.Forget(key)
		return
	}

	// This controller retries 5 times if something goes wrong. After that, it stops trying.
	if c.queue.NumRequeues(key) < 5 {
		klog.Infof("Error syncing pod %v: %v", key, err)

		// Re-enqueue the key rate limited. Based on the rate limiter on the
		// queue and the re-enqueue history, the key will be processed later again.
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	// Report to an external entity that, even after several retries, we could not successfully process this key
	runtime.HandleError(err)
	klog.Infof("Dropping pod %q out of the queue: %v", key, err)
}

// Run begins watching and syncing.
func (c *Controller) Run(ctx context.Context, workers int) {
	defer runtime.HandleCrash()

	// Let the workers stop when we are done
	defer c.queue.ShutDown()
	klog.Info("Starting Pod controller")

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(ctx.Done(), c.podsSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, ctx.Done())
	}

	<-ctx.Done()
	klog.Info("Stopping Pod controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}
