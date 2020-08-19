# Vedette Concept

Vedette is all about linking deployments that require something to something that provides this something.  
-> I now this is very abstract, please bear with me and read the intro just below.

or **tldr:**  
Your app needs something, Vedette gives you something fitting your requirements and replaces it, when your requirements change.

## Intro

Let's say you want to deploy wordpress. Wordpress needs a MySQL or MariaDB database, or at least something that speaks mysql protocol on the wire.

Now depending on your cloud provider, uptime requirements, whether it's just a staging deployment or if you want to test alternatives, you might find yourself in the situation that you want to swap your little out-of-cluster MariaDB server (that was carefully setup by yourself) with an Operator on Kubernetes or with AWS RDS or Google Cloud SQL.

This is the point where you start looking through the wordpress documentation and whatever you use to install it on Kubernetes (Helm Chart, Static Deployment, ...) to find out with knob to turn to make it use another DB backend.

With Vedette this process should become easier. Your application just needs to declare "I want something speaking mysql protocol" and Vedette will connect it to something that speaks mysql protocol.

If you want to change it - no problem

Just change your apps requirements to "I want something speaking mysql protocol, that does not run on my cluster!" or change the defaults for less specific requests.

## Examples

## API

### InterfaceProvisioner

An `InterfaceProvisioner` will automatically create new `InterfaceInstances` when `InterfaceClaims` cannot be Bound to an existing instance.  
Vedette provides multiple different provisioners.

A `ClusterInterfaceProvisioner` can be used to Provision Instances across a whole Kubernetes Cluster.

#### Out-of-band

In a very simple case, the `InterfaceProvisioner` is just defined with some out-of-band/custom controller, that will bind `InterfaceClaims` to this class.
If you don't need automation yet, you can also create a `dave-checks-these-after-lunch` `InterfaceProvisioner` and let Dave handle the work.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceProvisioner
metadata:
  name: awesome.postgres.sql
  namespace: default
priority: 1000    # priority of this InterfaceProvisioner relative to other provisioners
capabilities:
- db.sql.postgres # Database type
- net.tls         # is TLS encrypted
outOfBand: {}
```

#### Static

A static provisioner will solve each claim, by issuing the same static data to all of them.
In this example, we have a wildcard certificate that we want to share to anyone needing a wildcard cert for `company.com`.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceProvisioner
metadata:
  name: company-com-tls
  namespace: default
  labels:
    vedette.io/env: prod
priority: 50 # priority of this InterfaceProvisioner relative to other provisioners
capabilities:
- certificate
- certificate.wildcard
- company.com
static:
  credentials:
  - name: company-com-tls
    secretRef:
      name: company-com-wildcard-tls
```

#### Mapping

The mapping provisioner creates a new instance of a custom object to fulfill a claim. It can be used to offload work to other operators like KubeDB, without requiring the implementation of your own provisioner.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceProvisioner
metadata:
  name: staging.postgres.sql
  namespace: default
  labels:
    vedette.io/env: staging
priority: 50 # priority of this InterfaceProvisioner relative to other provisioners
capabilities:
- db.sql.postgres # Database type
- net.tls         # is TLS encrypted
mapping:
  # object to create to fulfill the claim
  template: |
    apiVersion: kubedb.com/v1alpha1
    kind: Postgres
    metadata: {} # name and namespace are set by Vedette
    spec:
      version: "10.2-v1"
      storageType: Durable
      storage:
        storageClassName: "standard"
        accessModes:
        - ReadWriteOnce
        resources:
          requests:
            storage: {{.Capacity.Storage}}
      terminationPolicy: DoNotTerminate
  credentials:
  - name: app-user
    # KubeDB will auto-generate this secret for us.
    # name is evaluated from the given go-template.
    fromSecret:
      name: '{{.Name}}-auth'
  config:
  - name: connection-data
    # arbitrary key -> go-template mappings
    fromTemplateMap:
      # KubeDB will create a Service with the same name as the
      # Postgres object itself.
      host: '{{.Name}}'
      port: '5432'
      database: postgres # default db
```

### InterfaceInstance

`InterfaceInstance` is _something_ that an InterfaceClaim can bind to. Think of it like a `PersistentVolume`, but it's an Interface that provides _something_. This can be a SQL Database, a REST API, or a complete application installation.

```yaml
apiVersion: core.vedette.io/v1alpha1
kind: InterfaceInstance
metadata:
  name: postgresql-01
  namespace: my-app
  labels:
    vedette.io/protocol: postgresql
    vedette.io/app: postgresql
    vedette.io/env: staging
  generation: 1
spec:
  # responsible provisioner instance (optional)
  provisionerName: awesome.postgres.sql
  # provided capabilities
  capabilities:
  - db.sql.postgres # Database type
  - net.tls         # is TLS encrypted
  # provided capacity
  capacity:
    storage: 50Gi
  # Delete this instance when the Claim is removed.
  # Alternative: Retain
  reclaimPolicy: Delete
  secrets:
  - name: postgresql-01-admin-credentials
  configMaps:
  - name: postgresql-01-connection-data
status:
  observedGeneration: 1
  phase: Bound
  conditions:
  - type: Ready
    status: 'True'
    reason: AllComponentsReady
    message: All components report ready.
  - type: Bound
    status: 'True'
    reason: Bound
    message: Bound to my-app-db claim.
```

### InterfaceClaim

`InterfaceClaims` define, what your application needs from a dependency and how it wants to consume secrets and configuration to use an `InterfaceInstance`.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceClaim
metadata:
  name: my-app-db
  namespace: my-app
  generation: 1
spec:
  # capacity requirements, need to be met equally or more
  capacity:
    storage: 50Gi
  # required capabilities of the interface implementation
  capabilities:
  - db.sql.postgres
  - net.tls
  # sub select specific instances
  selector:
    matchingLabels:
      vedette.io/env: staging
  configMaps:
  - name: connection-data
    # config keys will be available as environment variables with the given prefix.
    # e.g. POSTGRES_HOST, POSTGRES_PORT
    envFrom:
      prefix: POSTGRES_
  secrets:
  - name: admin-credentials
    # secrets will be mounted into the container file system on the given path.
    volumeMount:
      path: /var/secrets/database
status:
  observedGeneration: 1
  phase: Bound
  conditions:
  - type: Bound
    status: 'True'
    reason: Bound
    message: Bound to postgresql-01 instance.
```

#### Consuming Capabilities from a Pod

To make use of capabilities from your application, all you have to do is add a reference to the `InterfaceClaim` into your `Deployment` annotations:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app-db
  annotations:
    vedette.io/claim: my-wordpress-db
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
```

Vedette will automatically add needed mounts and env vars to consume the `Secrets`, `ConfigMaps`, etc that the Claim is providing and a controller will automatically update your deployment if the Claim itself is updated.

For the `InterfaceClaim` above, it will look like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app-db
  annotations:
    vedette.io/claim: my-wordpress-db
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.14.2
        ports:
        - containerPort: 80
# --- inserted by Vedette (start)
        envFrom:
        - configMapRef:
            name: postgresql-01-connection-data
          prefix: POSTGRES_
        volumeMounts:
        - name: my-app-db-app-user
          mountPath: /var/secrets/database
          readOnly: true
      volumes:
      - name: my-app-db-app-user
        secret:
          secretName: postgresql-01-admin-credentials
# --- inserted by Vedette (end)
```

#### InterfaceClaim matches multiple InterfaceProvisioners

If multiple `InterfaceProvisioners` can provision an `InterfaceInstance` that provides all capabilities for a Provider, the Provider with the highest Priority is chosen.

In case of multiple `InterfaceProvisioners` with the same Priority, `InterfaceProvisioners` take precedence over `ClusterInterfaceProvisioners`.

If there are still multiple matches the `InterfaceClaim` it will remain `Unbound` and a warning will be logged onto the `Claim`.  
The ambiguity needs to be resolved before the `InterfaceClaim` is bound to a backing instance.

## Recommended Labels

Similar to Kubernetes, Vedette defines a few common labels for `InterfaceClass` objects, to make working with multiple classes a bit less annoying.

|Key|Description|Example|Type|
|---|-----------|-------|----|
|vedette.io/protocol|The main communication protocol used.|`postgres`|string|
|vedette.io/env|Environment the class is intendet for.|`prod`, `staging`|string|
|vedette.io/app|Application.|`prod`, `staging`|string|

## Open Issues

### What if multiple Classes can fulfill the same Claim?
