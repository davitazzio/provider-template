/*
Copyright 2020 The Crossplane Authors.

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

package mytype

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/provider-template/apis/sample/v1alpha1"
	apisv1alpha1 "github.com/crossplane/provider-template/apis/v1alpha1"
	"github.com/crossplane/provider-template/internal/features"
)

const (
	errNotMyType    = "managed resource is not a MyType custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errNewClient = "cannot create new Service"
)

// A NoOpService does nothing.
type MyTypeService struct {
	Executed bool
}

var (
	newMyTypeService = func(_ []byte) (*MyTypeService, error) { return &MyTypeService{Executed: false}, nil }
)

// Setup adds a controller that reconciles MyType managed resources.
func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := managed.ControllerName(v1alpha1.MyTypeGroupKind)

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), apisv1alpha1.StoreConfigGroupVersionKind))
	}

	opts := []managed.ReconcilerOption{
		managed.WithExternalConnecter(&connector{
			kube:         mgr.GetClient(),
			usage:        resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
			newServiceFn: newMyTypeService,
			logger:       o.Logger}),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithPollInterval(o.PollInterval),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithConnectionPublishers(cps...),
	}

	if o.Features.Enabled(features.EnableAlphaManagementPolicies) {
		opts = append(opts, managed.WithManagementPolicies())
	}

	// o.Logger.Debug("ciao mamma 2 (siamo dentro a setup)")

	r := managed.NewReconciler(mgr, resource.ManagedKind(v1alpha1.MyTypeGroupVersionKind), opts...)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1alpha1.MyType{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	logger       logging.Logger
	kube         client.Client
	usage        resource.Tracker
	newServiceFn func(creds []byte) (*MyTypeService, error)
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.MyType)
	if !ok {
		return nil, errors.New(errNotMyType)
	}

	// c.logger.Debug("mamma ciao sono dentro connect")

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	svc, err := c.newServiceFn(data)
	if err != nil {
		return nil, errors.Wrap(err, errNewClient)
	}
	cr.Status.SetConditions(v1.Available())

	return &external{service: svc, logger: c.logger}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	// A 'client' used to connect to the external resource API. In practice this
	// would be something like an AWS SDK client.
	logger  logging.Logger
	service *MyTypeService
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.MyType)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotMyType)
	}

	cr.Status.SetConditions(v1.ReconcileSuccess())
	// These fmt statements should be removed in the real implementation.

	// c.logger.Debug("mamma sono dentro a observe")
	fmt.Printf("Observing ciao mamma: %+v", cr)

	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: c.service.Executed,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: true,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.MyType)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotMyType)
	}
	c.logger.Debug("Creating mamma: sono dentro la create")

	temp := fmt.Sprintf("MAMMA STO CREANDO LA RISORSA %s, CHE HA COME NODEADDRESS %s e come RESOURCE TO DOWNLOAD %s", cr.Name, cr.Spec.ForProvider.NodeAddress, cr.Spec.ForProvider.ResourceToDownload)

	c.logger.Debug(temp)

	// c.logger.Debug("Creating mamma: %+v", cr)

	c.logger.Debug("mamma ora setto la risorsa come available")

	// fanno la stessa cosa
	cr.Status.SetConditions(v1.Available())
	cr.SetConditions(v1.Available())

	cr.SetConditions(v1.ReconcileSuccess())

	// connectSSH(cr.Spec.ForProvider.NodeAddress, c.logger)

	sendHTTPReq(cr.Spec.ForProvider.NodeAddress, c.logger)
	c.service.Executed = true
	meta.SetExternalCreateSucceeded(cr.GetObjectMeta(), time.Now())

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.MyType)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotMyType)
	}

	fmt.Printf("Updating: %+v", cr)

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.MyType)
	if !ok {
		return errors.New(errNotMyType)
	}

	cr.SetDeletionPolicy(v1.DeletionDelete)

	fmt.Printf("Deleting: %+v", cr)

	return nil
}

func sendHTTPReq(hostAddress string, logger logging.Logger) {
	logger.Debug("Invio la richiesta a")
	logger.Debug(hostAddress)
	resp, err := http.Get(hostAddress + "/")
	if err != nil {
		logger.Debug("ERRORE NELLA RICHIESTA")
		logger.Debug(err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug("Errore nella lettura del body")
		logger.Debug(err.Error())
	}
	logger.Debug(string(body))
}

// func connectSSH(hostAddress string, logger logging.Logger) {

// 	// v, err := os.ReadFile("/home/datavix/.ssh/id_rsa") //read the content of file
// 	// if err != nil {
// 	// 	fmt.Println(err)
// 	// 	return
// 	// }
// 	// v_string :='-----BEGIN OPENSSH PRIVATE KEY-----
// 	// b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABlwAAAAdzc2gtcn
// 	// NhAAAAAwEAAQAAAYEAxTT4dr/mJ95/tfaKZNyVEmYb7uuvor82QEk13p66bFANkUEtwwJt
// 	// SZ/Bq3wGp/oPWMFUsHk/v6v0dyvBrB93ztVsghFqCIDG3RbQPnUOljivcbDy8VuS1PfHFe
// 	// UQqD1dYY1l2iEOv0+2kuMbkAFQjtE09Dd3u98s3aKWlGhuHqchJqweFrf6OJ7dtMyDhERS
// 	// bp4x7thwkhWDJSfbHxapv4p9Ltdns1L1nQITUplR7aSfJylWtLVT0zS2+emiCjAuUcK0pR
// 	// gI5buvUUsCean6jinQ09dao7qzp4NNQMpvmxBYupF9ouM5+4qr5isxsavy7bW8MJyz0zpf
// 	// ax71kqfwrvb60Do/oYoKRkSI9M0AY9GWAkaAG4g9roQ8oPMqq0l+IfOv2Oc8F+6r2xWK2p
// 	// ZSk6TYEp0b3Z0KEWDvkJyNRrat1MwSWBupiVnsf4Fg0CoNOrtxmjK+weSN9LITVhUnhj5U
// 	// MktQHaZIvtR1RISP6jiriKqtfkVswGIyiXl5+XrVAAAFmJo2zWmaNs1pAAAAB3NzaC1yc2
// 	// EAAAGBAMU0+Ha/5ifef7X2imTclRJmG+7rr6K/NkBJNd6eumxQDZFBLcMCbUmfwat8Bqf6
// 	// D1jBVLB5P7+r9Hcrwawfd87VbIIRagiAxt0W0D51DpY4r3Gw8vFbktT3xxXlEKg9XWGNZd
// 	// ohDr9PtpLjG5ABUI7RNPQ3d7vfLN2ilpRobh6nISasHha3+jie3bTMg4REUm6eMe7YcJIV
// 	// gyUn2x8Wqb+KfS7XZ7NS9Z0CE1KZUe2knycpVrS1U9M0tvnpogowLlHCtKUYCOW7r1FLAn
// 	// mp+o4p0NPXWqO6s6eDTUDKb5sQWLqRfaLjOfuKq+YrMbGr8u21vDCcs9M6X2se9ZKn8K72
// 	// +tA6P6GKCkZEiPTNAGPRlgJGgBuIPa6EPKDzKqtJfiHzr9jnPBfuq9sVitqWUpOk2BKdG9
// 	// 2dChFg75CcjUa2rdTMElgbqYlZ7H+BYNAqDTq7cZoyvsHkjfSyE1YVJ4Y+VDJLUB2mSL7U
// 	// dUSEj+o4q4iqrX5FbMBiMol5efl61QAAAAMBAAEAAAGALkFJKPNETOQtgNThq5woc/8SvL
// 	// S3xrzCQQxa8As7bzXMpN4MmYGreBoaXzpRRluK93arQlRCLVcsGTqga9qaq58YGx7yB6oK
// 	// 2ucjs46ZvAbyMcC/Dvj7ZOv0HJDUmh2AlmXHtsTLtHhCOsw9lgaU6lasLL8I3L5RQ/ADkS
// 	// 44bASn7C3xRcNj052Bo4tXqrGqwwrka+EE8GLO1qt1RCK48HYPfCnmhyNlfCz1MsnG825K
// 	// LTGPRoYEchTaeR5BVVHs1eFzTSfry+3f9m1AsNaG+LPsXUJ9LukZqvkfcTOqW+MA3TewB6
// 	// SHdBNKBJr3Xenhn6LueDmfuuu8UY7c/wvJbIbGW7ryOAVvjejUJZXAjkyKm8e7ViGql1Pj
// 	// jGXssw/bhKcSFpL1iWD9VZlBeHmSAXPfwdTmlrP6u0oDY3R0rb6PwCqs5Jshbdla2zcoeW
// 	// qVW4tPaK11BwR1M3slelsMQo3l+guk3jccLbdf592Ellwb5T1hUIztSCt8mgpi0dLRAAAA
// 	// wQCKof8tfs7755yEFdXe1nuKC5M0Fn1n6dKM6pyPwtfd6poeHb5uae0bXhOaLs0n1Tn13I
// 	// wcSkir6EUwSnmV7xbrZxThDCn68vbX7qnqFwnpK4WMeZdjGkmI/k5tFCZxXAzElhS2/7KP
// 	// c2or9Pzqj2Qygaox1ztHPwM6qz+6Dsd5Ur8BPIjs1AoH+u4v3IH/d1oWR6wemKd+qIdE51
// 	// hSX0taYm+KTf3sEr54GE5aupN9EtIYJ0x5KsNixueN8o6WEMgAAADBANghtnSAZeM8VL3U
// 	// s+UkgjKuq3TQ85qAJab9Euge6fmAbgRL/y0JAyPgj+k7gVMQ8r2X0RZcjnyEhsbGruan6c
// 	// k6I7SvHuZ+712iWPEXpXFXBPgB3LAy/fUO55UrlwZGaJqdsIJsLnP65pxV/bLH0m4M/NTa
// 	// VLOl1wESjxsorpdJknE/DR6xpdv2ejDTA8/Dhk1U/FPyuFLTUKAwmyVTUiNR1O7hrUwkKl
// 	// XJNQze4Yk74Q/zxL1EBYCVoymRntjdsQAAAMEA6ZWU+yRa2t3ZmUzHRyoWzIm4oWflQPCR
// 	// fWURM5vroJCzFp80Xq+Q6umSXJrjOn3QaWWqd06W97vKuQSEtZJHL0cwxbIbcp32XC9CA8
// 	// BxxlLMbDM1RLABRKXkmRwDwlBNiA8TqmpqBtFdNOEZfXlIYoxut3gi69IDKIq4JoyFBXb3
// 	// WJGevEjRfsT23gVuV1qV9rYNWqdEinreHL8sSCWtEupsVkrBxJS+anNIH73jcLfOW8x2by
// 	// feTFbTI5Xat0RlAAAAHGRhdGF2aXhAZHRhenppb2xpLWt1YmVybmV0ZXMBAgMEBQY=
// 	// -----END OPENSSH PRIVATE KEY-----'
// 	// logger.Debug("CONVERTO LA CHIAVE IN BYTE")

// 	// v := []byte(v_string)
// 	// signer, err := ssh.ParsePrivateKey(v)
// 	// if err != nil {
// 	// 	logger.Debug("errore nel parsing della chiave")
// 	// 	logger.Debug(err.Error())
// 	// }

// 	logger.Debug("ORA MI COLLEGO AL CLIENT")
// 	config := &ssh.ClientConfig{
// 		User: "datavix",
// 		Auth: []ssh.AuthMethod{
// 			ssh.Password("datavix"),
// 		},
// 		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
// 	}

// 	logger.Debug("ORA FACCIO DIAL")

// 	client, err := ssh.Dial("tcp", hostAddress+":22", config)
// 	if err != nil {
// 		logger.Debug("Failed to dial: ")
// 		logger.Debug(err.Error())
// 		// os.Exit(-1)
// 	}
// 	defer client.Close()

// 	session, _ := client.NewSession()
// 	defer session.Close()

// 	// file, _ := os.Open("prova2.txt")
// 	// defer file.Close()
// 	// stat, _ := file.Stat()

// 	// wg := sync.WaitGroup{}
// 	// wg.Add(1)

// 	// go func() {
// 	// 	hostIn, err := session.StdinPipe()
// 	// 	print(err.Error())
// 	// 	defer hostIn.Close()
// 	// 	_, err = fmt.Fprintf(hostIn, "C0664 %d %s\n", stat.Size(), "filecopyname")
// 	// 	print(err.Error())
// 	// 	io.Copy(hostIn, file)
// 	// 	fmt.Fprint(hostIn, "\x00")
// 	// 	wg.Done()
// 	// }()

// 	// session.Shell()
// 	logger.Debug("ORA SCRIVO IL FILE IN REMOTO")
// 	err = session.Run("echo pippo >> prova.txt")
// 	if err != nil {
// 		logger.Debug("ERRORE NELLA SCRITTURA DEL FILE")
// 		print(err.Error())
// 	}
// 	// print(err.Error())
// 	// wg.Wait()

// }
