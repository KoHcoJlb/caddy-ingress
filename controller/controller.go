package controller

import (
	"context"
	"fmt"
	networking "k8s.io/api/networking/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"time"
)

func skipIngress(i *networking.Ingress) bool {
	_, ok := i.GetAnnotations()["kohcojlb.caddy-ingress-proxy/disable"]
	return ok
}

type Controller struct {
	client      *kubernetes.Clientset
	addRoute    func(string)
	removeRoute func(string)
}

func New(kubeconfigPath string, addRoute func(route string), removeRoute func(string)) (*Controller, error) {
	c := Controller{addRoute: addRoute, removeRoute: removeRoute}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	c.client, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

func (c *Controller) Start(ctx context.Context) {
	go c.worker(ctx)
}

func (c *Controller) worker(ctx context.Context) {
	informer := informers.NewSharedInformerFactory(c.client, 0*time.Second)

	ingressInformer := informer.Networking().V1().Ingresses().Informer()
	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if i := obj.(*networking.Ingress); !skipIngress(i) {
				c.add(i)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if i := oldObj.(*networking.Ingress); !skipIngress(i) {
				c.remove(i)
			}
			if i := newObj.(*networking.Ingress); !skipIngress(i) {
				c.add(i)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if i := obj.(*networking.Ingress); !skipIngress(i) {
				c.remove(i)
			}
		},
	})
	ingressInformer.Run(ctx.Done())
}

func (c *Controller) add(ingress *networking.Ingress) {
	for _, rule := range ingress.Spec.Rules {
		c.addRoute(rule.Host)
	}
}

func (c *Controller) remove(ingress *networking.Ingress) {
	for _, rule := range ingress.Spec.Rules {
		c.removeRoute(rule.Host)
	}
}
