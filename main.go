package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"k8s.io/component-base/metrics/legacyregistry"
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // load all the prometheus client-go plugin
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration

	pulpoclient "github.com/aojea/kubernetes-controllers-workshop/apis/generated/clientset/versioned"
	pulpoinformersfactory "github.com/aojea/kubernetes-controllers-workshop/apis/generated/informers/externalversions"
	pulpoinformers "github.com/aojea/kubernetes-controllers-workshop/apis/generated/informers/externalversions/apis/v1alpha1"
	pulpolisters "github.com/aojea/kubernetes-controllers-workshop/apis/generated/listers/apis/v1alpha1"
)

var (
	kubeconfig         string
	bindMetricsAddress string
	ready              atomic.Bool
	leaseLockName      string
	leaseLockNamespace string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	flag.StringVar(&bindMetricsAddress, "bind-metrics-address", "0.0.0.0:10550", "The IP address and port for the metrics server to serve on, defaulting to 0.0.0.0:10550")
	flag.StringVar(&leaseLockName, "lease-lock-name", "mycontroller", "the lease lock resource name")
	flag.StringVar(&leaseLockNamespace, "lease-lock-namespace", "default", "the lease lock resource namespace")

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
		// If the Pulpo runs inside a cluster it can use the InCluster configuration
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

	// creates the CRD clientset
	crdClientset, err := pulpoclient.NewForConfig(config)
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

	controllerMux := mux.NewPathRecorderMux("mycontroller")
	controllerMux.Handle("/metrics", legacyregistry.Handler())
	controllerMux.HandleFunc("/readyz", func(rw http.ResponseWriter, req *http.Request) {
		if ready.Load() {
			rw.WriteHeader(http.StatusOK)
		} else {
			http.Error(rw, "not ready", http.StatusInternalServerError)
		}
	})

	go func() {
		err := http.ListenAndServe(bindMetricsAddress, controllerMux)
		if err != nil {
			// ListenAndServer always returns an error on exit
		}
	}()

	factory := pulpoinformersfactory.NewSharedInformerFactory(crdClientset, 0)
	pulpoInfomer := factory.Pulpocon().V1alpha1().Pulpos()
	controller := NewController(crdClientset, pulpoInfomer)

	factory.Start(ctx.Done())
	ready.Store(true)

	run := func(ctx context.Context) {
		klog.Info("Controller loop...")
		go controller.Run(ctx, 5) // use 5 workers
		<-ctx.Done()
	}

	id := uuid.New().String()
	// we use the Lease lock type since edits to Leases are less common
	// and fewer objects in the cluster watch "all Leases".
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseLockName,
			Namespace: leaseLockNamespace,
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	// start the leader election code loop
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: lock,
		// IMPORTANT: you MUST ensure that any code you have that
		// is protected by the lease must terminate **before**
		// you call cancel. Otherwise, you could have a background
		// loop still running and another process could
		// get elected before your background loop finished, violating
		// the stated goal of the lease.
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// we're notified when we start - this is where you would
				// usually put your code
				run(ctx)
			},
			OnStoppedLeading: func() {
				// we can do cleanup here
				klog.Infof("leader lost: %s", id)
				os.Exit(0)
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == id {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	})

}

// Controller demonstrates how to implement a controller with client-go.
type Controller struct {
	client           pulpoclient.Interface
	queue            workqueue.TypedRateLimitingInterface[string]
	pulpoLister      pulpolisters.PulpoLister
	pulpoSynced      cache.InformerSynced
	workerLoopPeriod time.Duration
}

// NewController creates a new Controller.
func NewController(client pulpoclient.Interface, pulpoInfomer pulpoinformers.PulpoInformer) *Controller {
	c := &Controller{
		client:      client,
		pulpoLister: pulpoInfomer.Lister(),
		pulpoSynced: pulpoInfomer.Informer().HasSynced,
		queue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[string](),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: "mycontroller",
			},
		),
		workerLoopPeriod: time.Second,
	}
	pulpoInfomer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Infof("Pulpo %s added", key)
				c.queue.Add(key)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(cur)
			if err == nil {
				klog.Infof("Pulpo %s updated", key)
				c.queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				klog.Infof("Pulpo %s deleted", key)
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
	klog.Infof("processing Pulpo %s", key)
	start := time.Now()
	defer func() {
		klog.Infof("finished to process Pulpo %s, it took %v", key, time.Since(start))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Infof("Failed to split meta namespace cache key %s", key)
		return err
	}
	pulpo, err := c.pulpoLister.Pulpos(ns).Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// This means the pod no longer exist in the informer cache
			// the cache is the eventual consistent state of the cluster
			// so all the reconcilation related to the pod deletion goes here
			// ... do delete stuff ...
			klog.Infof("Pulpo %s does not exist anymore\n", key)
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
	if pulpo.DeletionTimestamp != nil {
		klog.Infof("Pulpo %s is being deleted\n", key)
		return nil
	}

	// don't mutate the object
	newPulpo := pulpo.DeepCopy()
	newPulpo.Status.TentaculosDisponibles = *newPulpo.Spec.Tentaculos
	_, err = c.client.PulpoconV1alpha1().Pulpos(ns).UpdateStatus(context.Background(), newPulpo, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	// Note that you also have to check the uid if you have a local controlled resource, which
	// is dependent on the actual instance, to detect that a Pulpo was recreated with the same name
	klog.Infof("Sync/Add/Update for Pulpo %s\n", key)
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
	klog.Info("Starting Pulpo controller")

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(ctx.Done(), c.pulpoSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, ctx.Done())
	}

	<-ctx.Done()
	klog.Info("Stopping Pulpo controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}
