# Especificación de implementación — Metanodo (DFSha)

Contexto para el agente: este es el servidor "Metanodo" de un sistema de archivos distribuido (DFSha) para un curso universitario de Sistemas Distribuidos, escrito en Go. El Metanodo es el componente de metadata: no almacena bytes de archivos (eso lo hacen los DataNodes, otro binario, fuera de este alcance), solo sabe qué archivos existen, en qué bloques están partidos, en qué DataNodes vive cada bloque, y quién es cada usuario.

## Estado actual del repositorio

Ya existe, en `internal/server/metanode/store/`:
- `types.go`
- `store.go`
- `memory_store.go`
- `memory_store_test.go`

No modifiques esos archivos salvo que se indique explícitamente abajo. Todo lo nuevo va en paquetes separados que dependen de `store` solo a través de la interfaz `Store`, nunca de `MemoryStore` directamente.

## Estructura de paquetes a crear

```
cmd/metanode/
    main.go

internal/server/metanode/
    store/
        nfs_store.go          (nuevo, ver sección 6)
        nfs_store_test.go
    service/
        users.go
        users_test.go
        files.go
        files_test.go
        datanodes.go
        datanodes_test.go
        failover.go
        failover_test.go
    api/
        rest/
            router.go
            auth_handlers.go
            file_handlers.go
            dir_handlers.go
            middleware.go
        grpc/
            heartbeat_service.go
```

Dependencias Go a usar (añadir a `go.mod`):
- `golang.org/x/crypto/bcrypt`
- `github.com/golang-jwt/jwt/v5`
- `github.com/google/uuid`
- Librería de router HTTP: usar `net/http` estándar con `http.ServeMux` (Go 1.22+ soporta patrones con método y parámetros, `"POST /auth/login"`), salvo que el proyecto ya tenga `chi` o `gin` como dependencia — en ese caso usar lo que ya esté.

## 1. `service/users.go`

Reglas de validación:
- `username`: 1 a 16 caracteres alfanuméricos.
- `password` en texto plano: 6 a 72 caracteres alfanuméricos.

**IMPORTANTE — límite de bcrypt**: `golang.org/x/crypto/bcrypt` falla con `bcrypt.ErrPasswordTooLong` si el input supera 72 bytes. Dado que originalmente se había definido un máximo de 92 caracteres, el límite real aplicado aquí debe ser 72, no 92. No pre-hashear con SHA256 salvo que se indique lo contrario explícitamente en una iteración futura — por ahora, limitar a 72 es suficiente y más simple.

Funciones requeridas:

```go
func Register(s store.Store, username, plaintextPassword string) error
func Login(s store.Store, username, plaintextPassword string) (jwtToken string, err error)
func ValidateJWT(tokenString string) (username string, err error)
func GenerateCapabilityToken(username string, blockIDs []string, ttl time.Duration) (string, error)
func ValidateCapabilityToken(tokenString string) (username string, blockIDs []string, err error)
```

- `Register`: valida longitudes → si falla, error descriptivo sin wrappear como panic. Verifica que no exista ya el username (`store.GetUser`). Hashea con `bcrypt.GenerateFromPassword` (cost por defecto, `bcrypt.DefaultCost`). Llama a `store.CreateUser` con el hash, cuota usada en 0.
- `Login`: busca usuario, `bcrypt.CompareHashAndPassword`. Si coincide, genera JWT con claim `sub` = username y expiración configurable (variable de entorno o constante, default razonable para pruebas, ej. 5 minutos — no 30 segundos, eso es demasiado corto para el JWT de sesión general; los 30s de la tabla de parámetros del equipo corresponden al capability token, no a este).
- `GenerateCapabilityToken`: JWT separado, de vida corta (usar el TTL que pase el caller, pensado para ~30s), con claims `sub` (username) y `block_ids` (lista de block IDs autorizados). Lo usa el cliente para hablar directo con los DataNodes.
- Firma de ambos JWT con una clave secreta cargada de variable de entorno (`METANODE_JWT_SECRET`), nunca hardcodeada en el código.

Tests table-driven cubriendo: registro exitoso, username duplicado, username fuera de rango, password fuera de rango, login exitoso, login con password incorrecta, login con usuario inexistente, JWT válido, JWT expirado, JWT con firma inválida.

## 2. `service/files.go`

Parámetros fijos (constantes, en un archivo `service/constants.go`):

```go
const (
    BlockSizeBytes      = 1 * 1024 * 1024  // 1 MB
    ReplicationFactor   = 3
    MaxUserQuotaBytes   = 2 * 1024 * 1024 * 1024 // 2 GB
)
```

Funciones requeridas:

```go
type BlockPlacement struct {
    BlockID   string
    DataNodes []string // direcciones de los DataNodes asignados a esta réplica
}

func InitPut(s store.Store, username, path string, sizeBytes int64) ([]BlockPlacement, error)
func CompletePut(s store.Store, username, path string, sha256Hash string) error
func ResolveGet(s store.Store, username, path string) (blocks []store.BlockMetadata, sha256Hash string, err error)
func ListDir(s store.Store, username, dirPath string) ([]store.FileMetadata, error)
func DeleteFile(s store.Store, username, path string) error
```

- `InitPut`: 1) calcular cuota: `store.GetUser(username).QuotaUsedBytes + sizeBytes > MaxUserQuotaBytes` → error de cuota excedida, sin tocar nada más. 2) calcular número de bloques: `ceil(sizeBytes / BlockSizeBytes)`. 3) para cada bloque, generar block ID con `uuid.NewString()`, y asignar `ReplicationFactor` DataNodes usando Round Robin sobre `store.ListAliveDataNodes()` (ver sección 3 para cómo se obtiene esa lista). 4) persistir la metadata del archivo y de cada bloque en el store (`store.CreateFile`, `store.SaveBlockLocation` por cada bloque). 5) devolver el `[]BlockPlacement` para que la capa API se lo entregue al cliente.
- `CompletePut`: se llama después de que el cliente terminó de subir todos los bloques; actualiza `QuotaUsedBytes` del usuario sumando `sizeBytes`, y guarda el hash SHA256 del archivo completo en su metadata.
- `ResolveGet`: valida ownership (ver sección 4) antes de devolver nada. Si no es el dueño, devolver error de autorización explícito, distinto de "no encontrado".
- `ListDir`: filtra los archivos de `store` cuyo `Path` empiece por `dirPath`, y cuyo `Owner == username` (hasta que se implemente ACL de grupos).
- `DeleteFile`: valida ownership, borra del store, resta `sizeBytes` de la cuota usada del usuario.

Round Robin: mantener un índice global (protegido por mutex, o atómico) que avanza cada vez que se asigna un bloque, nunca se reinicia a 0 entre llamadas, y da la vuelta (`% len(aliveNodes)`) al llegar al final de la lista.

## 3. `service/datanodes.go`

```go
func RegisterDataNode(s store.Store, id, address string) error
func ReceiveHeartbeat(s store.Store, id string, freeBytes int64) error
func StartHealthMonitor(s store.Store, checkInterval time.Duration, deadThreshold time.Duration)
```

- `StartHealthMonitor`: lanzar una goroutine con `time.Ticker` de `checkInterval` (usar 5s). En cada tick, recorrer todos los DataNodes registrados; si `time.Since(LastHeartbeat) > deadThreshold` (usar 15s = 3 × 5s), marcar `Alive = false`. Esta goroutine arranca una sola vez desde `cmd/metanode/main.go` al iniciar el proceso.
- `store.ListAliveDataNodes()` debe filtrar por el campo `Alive`, que esta goroutine es la única responsable de mantener actualizado.

## 4. Autorización (dentro de `service/files.go`, no un archivo aparte)

Antes de cualquier operación sobre un archivo existente (`ResolveGet`, `DeleteFile`, y el `ListDir` ya filtra por owner arriba), comparar `fileMetadata.Owner == username`. Si no coincide, devolver un error distinguible (por ejemplo, un error tipado `ErrForbidden`, distinto de `ErrNotFound`), para que la capa `api/rest` pueda mapearlo a HTTP 403 en vez de 404.

No implementar ACL de grupos en esta iteración salvo que se indique explícitamente — dejar un comentario `// TODO: soporte de grupos/ACL si el alcance lo permite` donde correspondería extenderlo (en la comparación de ownership de cada función).

## 5. `api/rest/`

Usar JSON para request/response bodies. Rutas y métodos:

| Método | Ruta | Body entrada | Respuesta éxito |
|---|---|---|---|
| POST | `/auth/register` | `{username, password}` | `204` |
| POST | `/auth/login` | `{username, password}` | `{token, expires_in}` |
| POST | `/files/put-init` | `{path, size_bytes}` | `{blocks: [{block_id, datanodes}], capability_token}` |
| POST | `/files/put-complete` | `{path, sha256}` | `204` |
| GET | `/files/{path}` | — | `{blocks: [...], sha256, capability_token}` |
| GET | `/files?dir={path}` | — | `[{name, size, owner}]` |
| DELETE | `/files/{path}` | — | `204` |
| POST | `/directories` | `{path}` | `204` |
| DELETE | `/directories/{path}` | — | `204` |

`middleware.go`: función `RequireAuth(next http.Handler) http.Handler` que lee el header `Authorization: Bearer <token>`, llama a `service.ValidateJWT`, y si es válido, inyecta el username en el `context.Context` de la request (`context.WithValue`) para que los handlers lo lean. Si falla, responder `401` sin llamar a `next`. Aplicar este middleware a todas las rutas excepto `/auth/register` y `/auth/login`.

Mapeo de errores de `service/` a códigos HTTP en cada handler:
- Error de validación (longitud, formato) → `400`
- Error de autenticación fallida → `401`
- `ErrForbidden` (no es el dueño) → `403`
- `ErrNotFound` → `404`
- Username/archivo ya existe → `409`
- Cuota excedida → `413` (Payload Too Large) o `403`, usar `403` por simplicidad semántica con autorización de recursos.

## 6. `store/nfs_store.go`

Segunda implementación de la interfaz `Store` (definida en `store/store.go`, no modificarla), que persiste en archivos JSON dentro de una carpeta montada por NFS/EFS en vez de en mapas de memoria.

```go
type NFSStore struct {
    basePath string // carpeta raíz montada, ej. /mnt/dfsha-shared
    mu       sync.RWMutex
}

func NewNFSStore(basePath string) *NFSStore
```

Archivos dentro de `basePath`:
- `files.json` — array de `FileMetadata`
- `blocks.json` — array de `BlockMetadata`
- `datanodes.json` — array de `DataNodeInfo`
- `users.json` — array de `UserInfo`

Patrón de cada método: adquirir el mutex correspondiente (lectura o escritura), leer el archivo JSON completo a memoria, operar, si es escritura volver a serializar y escribir el archivo completo (usar escritura atómica: escribir a un archivo temporal `*.tmp` y luego `os.Rename` sobre el definitivo, para evitar corrupción si el proceso muere a mitad de escritura). Implementar todos los métodos de la interfaz `Store` con esta misma estrategia.

## 7. `service/failover.go`

```go
func StartHeartbeatWriter(basePath, nodeID string, interval time.Duration)
func IsLeaderAlive(basePath string, deadThreshold time.Duration) bool
func TryAcquireLock(basePath, nodeID string, lockTTL time.Duration) bool
func RenewLock(basePath, nodeID string) error
```

- `StartHeartbeatWriter`: goroutine que, mientras el nodo actual sea el líder, escribe cada `interval` (5s) la hora actual en `basePath/heartbeat.txt`.
- `IsLeaderAlive`: lee `heartbeat.txt`, compara contra `time.Now()`; si la diferencia supera `deadThreshold` (15s), devuelve `false`.
- `TryAcquireLock`: intenta crear/leer `basePath/metanode.lock`. Formato del archivo: `{"owner": "<nodeID>", "timestamp": "<RFC3339>"}`. Si no existe, o existe pero su timestamp es más viejo que `lockTTL`, lo sobrescribe con el nodeID propio y devuelve `true`. Si existe y es reciente y pertenece a otro nodo, devuelve `false`.
- `RenewLock`: mientras el nodo sea el líder, reescribe `metanode.lock` con su propio nombre y la hora actual, en el mismo intervalo que el heartbeat.

Lógica en `cmd/metanode/main.go`: al arrancar, si el nodo está configurado como respaldo, lanzar una goroutine que cada `checkInterval` llama a `IsLeaderAlive`; si devuelve `false`, intenta `TryAcquireLock`; si lo consigue, este proceso empieza a levantar el servidor REST/gRPC (si no estaba ya corriendo) y a llamar `StartHeartbeatWriter` + renovación periódica del lock.

## 8. `cmd/metanode/main.go`

Orden de arranque:
1. Leer configuración (puerto REST, puerto gRPC, `METANODE_JWT_SECRET`, ruta del NFS compartido, si este nodo arranca como líder o como respaldo) desde variables de entorno.
2. Instanciar el `Store`: `NFSStore` si hay ruta NFS configurada, `MemoryStore` en caso contrario (para pruebas locales sin montar NFS todavía).
3. Si es líder: arrancar servidor HTTP (`api/rest`) y servidor gRPC (`api/grpc`), arrancar `StartHealthMonitor` (sección 3), arrancar `StartHeartbeatWriter` + renovación de lock (sección 7).
4. Si es respaldo: no levantar servidores todavía, solo arrancar la goroutine de vigilancia del heartbeat del líder (sección 7), que al detectar caída, ejecuta el paso 3 completo.

## Orden de implementación recomendado

1. `service/users.go` + tests (no depende de nada más nuevo).
2. `service/files.go` + tests, usando `MemoryStore` para probar.
3. `service/datanodes.go` + tests.
4. `api/rest/` completo, probado con `MemoryStore` y `curl`/Postman.
5. `store/nfs_store.go` + tests (puede probarse localmente con una carpeta normal del disco, simulando el montaje NFS).
6. `service/failover.go` + integración en `main.go`.
7. Prueba de extremo a extremo: dos procesos de Metanodo apuntando a la misma carpeta compartida, matar el que está activo, confirmar que el otro toma el control y la metadata sigue íntegra.