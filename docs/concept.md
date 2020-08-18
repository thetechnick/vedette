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

### InterfaceClaim

`InterfaceClaims` define, what your application needs from a dependency and how it wants to consume the connection informations.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceClaim
metadata:
  name: my-wordpress-db
spec:
  selector:
    matchingLabels:
      vedette.io/protocol: postgresql
  config:
  - name: connection-data
    # config keys will be available as environment variables with the given prefix.
    # e.g. POSTGRES_HOST, POSTGRES_PORT
    env:
      prefix: POSTGRES_
  credentials:
  - name: app-user
    # secrets will be mounted into the container file system on the given path.
    file:
      path: /var/secrets/database
      
status:
  phase: Bound
  credentials:
  - name: db-admin
    secret:
      name: my-wordpress-db
  endpoints:
  - name: postgres
    service:
      name: my-wordpress-db
  conditions:
  - type: Bound
    status: 'True'
```

### Consuming Capabilities from a Pod

To make use of capabilities from your application, all you have to do is add a reference to the `InterfaceClaim` into your `Deployment` annotations:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
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
  name: nginx-deployment
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
        env:
# --- inserted by Vedette (start)
        - name: POSTGRES_HOST
          value: "" # value from bound claim
        - name: POSTGRES_PORT
          value: "5432" # value from bound claim
        volumeMounts:
        - name: my-wordpress-db-app-user
          mountPath: /var/secrets/database
          readOnly: true
      volumes:
      - name: my-wordpress-db-app-user
        secret:
          secretName: my-wordpress-db-app-user
# --- inserted by Vedette (end)
```

### InterfaceClaim matches multiple InterfaceClasses

`InterfaceClaims` need to select only a single `InterfaceClass`.

If multiple `InterfaceClass` object match the specified selector and the `InterfaceClaim` is not yet `Bound` it will remain `Unbound` and a warning will be logged onto the `Claim`.  
The ambiguity needs to be resolved before the `InterfaceClaim` is bound to a backing instance.

If the `InterfaceClaim` is already `Bound`, nothing will happen.

### InterfaceClass

A `InterfaceClass` defines a type of capability and optional how this capability  is fulfilled.

#### Out-of-band

In a very simple case, the `InterfaceClass` is just defined with some out-of-band/custom controller, that will bind `InterfaceClaims` to this class.
If you don't need automation yet, you can also specify a `provisioner` like `dave-checks-these-after-lunch` and let Dave handle the work.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceClass
metadata:
  name: awesome.postgres.sql
  namespace: default
  labels:
    vedette.io/protocol: postgresql
    my-type: awesome
provisioner: my-awesome-postgres-controller
```

#### Static

`vedette.io/static` is a built-in provisioner that will solve each claim, by issuing the same static data to all of them.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceClass
metadata:
  name: prod.postgres.sql
  namespace: default
  labels:
    vedette.io/protocol: postgresql
    vedette.io/env: prod
    important: Dave - Don't touch!
provisioner: vedette.io/static
static:
  credentials:
  - name: app-user
    secret:
      name: prod-app-user
  config:
  - name: connection-data
    configMap:
      name: prod-database
```

#### Mapping

`vedette.io/mapping` is a built-in provisioner, that creates a new instance of a custom object to fulfill a claim. It can be used to offload work to other operators like KubeDB, without requiring the implementation of your own provisioner.

```yaml
apiVersion: vedette.io/v1alpha1
kind: InterfaceClass
metadata:
  name: staging.postgres.sql
  namespace: default
  labels:
    vedette.io/protocol: postgresql
    vedette.io/env: staging
provisioner: vedette.io/mapping
mapping:
  instance:
    # object to create to fulfill the claim
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
            storage: 50Mi
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

## Recommended Labels

Similar to Kubernetes, Vedette defines a few common labels for `InterfaceClass` objects, to make working with multiple classes a bit less annoying.

|Key|Description|Example|Type|
|---|-----------|-------|----|
|vedette.io/protocol|The main communication protocol used.|`postgres`|string|
|vedette.io/env|Environment the class is intendet for.|`prod`, `staging`|string|

## Open Issues

### What if multiple Classes can fulfill the same Claim?
