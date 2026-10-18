# Proyecto 1 de Sistemas Distribuidos : dfsha

Sistema de archivos distribuido por bloques. Un cliente interactivo (shell) habla con un
Metanodo (metadata, REST) y con varios DataNodes (bloques, gRPC). Cada bloque se replica
en 3 DataNodes y se cifra en disco.

## Ejecutar el proyecto

Se necesita Go (la versión está en `go.mod`). Para un clúster mínimo hacen falta 1 Metanodo
y al menos 3 DataNodes (factor de replicación 3).

### Variables de entorno

Metanodo:

| Variable | Obligatoria | Descripción |
|---|---|---|
| `METANODE_JWT_SECRET` | sí | Clave que firma las sesiones |
| `DFSHA_CAPABILITY_SECRET` | sí | Clave que firma los tokens de acceso a bloques |
| `DFSHA_CLUSTER_SECRET` | sí | Secreto compartido entre nodos |
| `METANODE_PORT` | no | Puerto REST (`8080`) |
| `METANODE_ID` | no | Identificador del nodo (`metanode-1`) |
| `METANODE_SHARED_DIR` | no | Carpeta compartida para persistir y hacer failover. Sin ella corre en memoria |
| `METANODE_LOCK_TTL` | no | Vigencia del lock del líder (`15s`) |

DataNode:

| Variable | Obligatoria | Descripción |
|---|---|---|
| `DFSHA_DATANODE_ID` | sí | Identificador único del nodo |
| `DFSHA_DATANODE_KEY` | sí | Clave AES-256 en hexadecimal (64 caracteres), la misma en todos los nodos |
| `DFSHA_CAPABILITY_SECRET` | sí | El mismo valor que en el Metanodo |
| `DFSHA_CLUSTER_SECRET` | sí | El mismo valor que en el Metanodo |
| `DFSHA_DATANODE_ADDR` | no | Dirección donde escucha (`:9000`) |
| `DFSHA_DATANODE_ADVERTISE_ADDR` | no | Dirección que ven los clientes (`localhost:<puerto>`) |
| `DFSHA_DATANODE_DIR` | no | Carpeta de bloques (`data/datanode`) |
| `DFSHA_METANODE_URL` | no | URL del Metanodo (`http://localhost:8080`) |
| `DFSHA_DATANODE_ALLOW_PLAINTEXT` | no | `1` guarda sin cifrar (solo desarrollo) |

Cliente:

| Variable | Descripción |
|---|---|
| `DFSHA_METANODE_URL` | Una o varias URL separadas por coma (`http://localhost:8080`) |
| `EDITOR` | Editor que usa `edit` (por defecto `nano`, luego `vi`) |

### Windows PowerShell

```powershell
$env:METANODE_JWT_SECRET = "cambiar-jwt"
$env:DFSHA_CAPABILITY_SECRET = "cambiar-capacidades"
$env:DFSHA_CLUSTER_SECRET = "cambiar-cluster"
go run .\cmd\metanode
```

En otra terminal, un DataNode (repetir con otro ID y puerto para los demás):

```powershell
$env:DFSHA_DATANODE_ID = "dn1"
$env:DFSHA_DATANODE_ADDR = "127.0.0.1:9101"
$env:DFSHA_DATANODE_ADVERTISE_ADDR = "127.0.0.1:9101"
$env:DFSHA_DATANODE_DIR = "data\dn1"
$env:DFSHA_DATANODE_KEY = -join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Maximum 256) })
$env:DFSHA_CAPABILITY_SECRET = "cambiar-capacidades"
$env:DFSHA_CLUSTER_SECRET = "cambiar-cluster"
go run .\cmd\datanode
```

Y el cliente:

```powershell
go run .\cmd\client
```

### Linux

```bash
export METANODE_JWT_SECRET=cambiar-jwt
export DFSHA_CAPABILITY_SECRET=cambiar-capacidades
export DFSHA_CLUSTER_SECRET=cambiar-cluster
export DFSHA_DATANODE_KEY=$(openssl rand -hex 32)   # la misma en los 3 DataNodes

go run ./cmd/metanode
```

En otras terminales, un DataNode por terminal (cambiar `1` por `2` y `3`). En cada una hay
que exportar los tres secretos con los mismos valores y la misma `DFSHA_DATANODE_KEY`:

```bash
DFSHA_DATANODE_ID=dn1 DFSHA_DATANODE_ADDR=127.0.0.1:9101 \
DFSHA_DATANODE_ADVERTISE_ADDR=127.0.0.1:9101 DFSHA_DATANODE_DIR=data/dn1 \
go run ./cmd/datanode
```

Y el cliente:

```bash
go run ./cmd/client
```

### Comandos del shell

| Comando | Descripción |
|---|---|
| `register`, `login`, `logout` | Gestión de la sesión |
| `ls [-R] [dir]`, `cd`, `pwd` | Navegar el sistema de archivos |
| `mkdir`, `rmdir` | Crear y borrar directorios |
| `rm [-r] [-f] <ruta>` | Borrar archivos y directorios |
| `put [-v] <local> [remota]` | Subir un archivo |
| `get [-v] <remota> [local]` | Descargar un archivo |
| `cat <archivo>` | Mostrar el contenido (verifica el hash antes de imprimir) |
| `edit <archivo>` | Editar con `$EDITOR` (por defecto `nano`) y subir si cambió |
| `nano <archivo>` | Igual que `edit`, pero fuerza `nano` |
| `help`, `clear`, `exit` | Utilidades |

`-v` muestra en qué DataNodes se guarda o se recupera cada bloque.

`edit` descarga el archivo a un directorio temporal privado, lo abre en el editor y, si
cambió, sube la nueva versión de forma atómica. Si otra sesión guardó mientras tanto, no
sobrescribe: avisa y deja tu versión en `<nombre>.dfsha-recovery`. Si el archivo no
existe, lo crea. Rechaza binarios, archivos vacíos y archivos de más de 5 MB.

### Alta disponibilidad del Metanodo

Con `METANODE_SHARED_DIR` apuntando a una carpeta compartida, el Metanodo guarda su estado
allí y puede haber varios nodos: uno es líder (`GET /health` responde 200) y los demás
esperan como respaldo. Si el líder cae, un respaldo toma el lock, carga el estado y pasa a
ser líder.

```bash
METANODE_ID=mn1 METANODE_PORT=8081 METANODE_SHARED_DIR=/tmp/dfs/shared go run ./cmd/metanode
METANODE_ID=mn2 METANODE_PORT=8082 METANODE_SHARED_DIR=/tmp/dfs/shared go run ./cmd/metanode
```

El cliente acepta varias URL: `DFSHA_METANODE_URL=http://localhost:8081,http://localhost:8082`.

### Limitaciones conocidas

- No hay TLS: los tokens y el secreto de clúster viajan sin cifrar.
- Los DataNodes aceptan una sola URL de Metanodo. Tras un failover hace falta un proxy que
  apunte al líder (usando `/health`) para que vuelvan a registrarse.
- El tamaño de bloque es de 1 MiB. `METANODE_BLOCK_SIZE` existe, pero el cliente usa 1 MiB
  fijo: no modificarlo.
- Los comandos `open`, `close`, `read` y `write` aún no están implementados.

## Pruebas

```bash
go test ./...
go test -race ./...
```

Las pruebas de integración (`internal/client/shell/integration_test.go`) levantan un
Metanodo y DataNodes reales dentro del propio test, no hace falta preparar nada.

## Estructura básica

```text
dfsha/
├── cmd/
│   ├── client/
│   ├── datanode/
│   └── metanode/
├── internal/
│   ├── client/
│   │   ├── api/
│   │   └── shell/
│   ├── proto/
│   └── server/
│       ├── datanode/
│       │   ├── auth/
│       │   ├── blockstore/
│       │   ├── crypto/
│       │   ├── grpcserver/
│       │   ├── heartbeat/
│       │   └── replication/
│       └── metanode/
│           ├── api/rest/
│           ├── service/
│           └── store/
├── logs/
└── proto/
```

El código se organiza dentro de `internal` por paquetes y responsabilidades.

Por ejemplo, `client/shell` se organiza de la siguiente forma:

```text
shell/
├── [HANDLERS]
├── [SESSION]
├── [CORE]
└── [OTHER]
```

`[HANDLERS]` contiene la ejecución de comandos, `[SESSION]` contiene la gestión de sesiones, `[CORE]` contiene la lógica y declaraciones principales del shell, y `[OTHER]` contiene funciones auxiliares.

En el servidor, `metanode` se divide en `store` (guardar y buscar metadata), `service`
(reglas de negocio) y `api/rest` (HTTP). `datanode` guarda los bloques cifrados
(`blockstore`, `crypto`), los sirve por gRPC (`grpcserver`), valida los tokens (`auth`),
copia los bloques a otros nodos (`replication`) y avisa que sigue vivo (`heartbeat`).

## Código gRPC generado

Los contratos gRPC viven en `proto/` y el código Go generado se versiona en el
repositorio bajo `internal/proto/`. Por eso, para compilar y ejecutar el
proyecto solo se necesita Go: no hay que instalar nada más.

```powershell
go build ./...
```

### Regenerar (solo si se edita un `.proto`)

Quien modifique un archivo `.proto` debe regenerar y commitear el resultado.
Se usa [buf](https://buf.build) con los plugins de Go. Instalación por única vez:

```powershell
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Generación (desde la raíz del proyecto):

```powershell
buf generate proto
```

El binario de los plugins queda en el `bin` de Go (`go env GOPATH\bin`), que
debe estar en el `PATH`.
