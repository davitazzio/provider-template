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
	"os"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/provider-template/apis/sample/v1alpha1"
	apisv1alpha1 "github.com/crossplane/provider-template/apis/v1alpha1"
	"github.com/crossplane/provider-template/internal/features"

	"golang.org/x/crypto/ssh"
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

	// cr.Status.SetConditions(v1.Available())
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
	cr.Status.SetConditions(v1.Available())

	// connectSSH(cr.Spec.ForProvider.NodeAddress, c.logger)

	c.service.Executed = true
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

func connectSSH(hostAddress string, logger logging.Logger) {

	// v, err := os.ReadFile("/home/datavix/.ssh/id_rsa") //read the content of file
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }
	v_string := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDFNPh2v+Yn3n+19opk3JUSZhvu66+ivzZASTXenrpsUA2RQS3DAm1Jn8GrfAan+g9YwVSweT+/q/R3K8GsH3fO1WyCEWoIgMbdFtA+dQ6WOK9xsPLxW5LU98cV5RCoPV1hjWXaIQ6/T7aS4xuQAVCO0TT0N3e73yzdopaUaG4epyEmrB4Wt/o4nt20zIOERFJunjHu2HCSFYMlJ9sfFqm/in0u12ezUvWdAhNSmVHtpJ8nKVa0tVPTNLb56aIKMC5RwrSlGAjlu69RSwJ5qfqOKdDT11qjurOng01Aym+bEFi6kX2i4zn7iqvmKzGxq/LttbwwnLPTOl9rHvWSp/Cu9vrQOj+higpGRIj0zQBj0ZYCRoAbiD2uhDyg8yqrSX4h86/Y5zwX7qvbFYrallKTpNgSnRvdnQoRYO+QnI1Gtq3UzBJYG6mJWex/gWDQKg06u3GaMr7B5I30shNWFSeGPlQyS1Adpki+1HVEhI/qOKuIqq1+RWzAYjKJeXn5etU= datavix@dtazzioli-kubernetes"

	logger.Debug("CONVERTO LA CHIAVE IN BYTE")

	v := []byte(v_string)
	signer, err := ssh.ParsePrivateKey(v)
	if err != nil {
		logger.Debug("errore nel parsing della chiave")
		logger.Debug(err.Error())
	}

	logger.Debug("ORA MI COLLEGO AL CLIENT")
	config := &ssh.ClientConfig{
		User: "datavix",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	logger.Debug("ORA FACCIO DIAL")

	client, err := ssh.Dial("tcp", hostAddress+":22", config)
	if err != nil {
		print("Failed to dial: ", err)
		os.Exit(-1)
	}
	defer client.Close()

	session, _ := client.NewSession()
	defer session.Close()

	// file, _ := os.Open("prova2.txt")
	// defer file.Close()
	// stat, _ := file.Stat()

	// wg := sync.WaitGroup{}
	// wg.Add(1)

	// go func() {
	// 	hostIn, err := session.StdinPipe()
	// 	print(err.Error())
	// 	defer hostIn.Close()
	// 	_, err = fmt.Fprintf(hostIn, "C0664 %d %s\n", stat.Size(), "filecopyname")
	// 	print(err.Error())
	// 	io.Copy(hostIn, file)
	// 	fmt.Fprint(hostIn, "\x00")
	// 	wg.Done()
	// }()

	// session.Shell()
	logger.Debug("ORA SCRIVO IL FILE IN REMOTO")
	err = session.Run("echo pippo >> prova.txt")
	if err != nil {
		print(err.Error())
	}
	// print(err.Error())
	// wg.Wait()

}
