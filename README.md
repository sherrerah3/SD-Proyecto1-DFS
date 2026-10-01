# Proyecto 1 de Sistemas Distribuidos : dfsha

## Ejecutar el proyecto (por el momento solo esta client)

### Windows PowerShell

```powershell
go run .\cmd\client
```

### Linux

```bash
go run ./cmd/client
```

## Scripts

### `scripts/inspect_codebase.py`

Se utiliza para visualizar la organización del código y analizar las dependencias entre funciones.

Recibe una carpeta y una subcarpeta dentro de `internal`.

Por ejemplo, para inspeccionar `internal/client/shell/`:

### Windows PowerShell

```powershell
python .\scripts\inspect_codebase.py client shell
```

### Linux

```bash
python3 ./scripts/inspect_codebase.py client shell
```

El script muestra la estructura del directorio y organiza los archivos según su responsabilidad. También identifica las funciones con dependencias y las funciones sin dependencias.

## Estructura básica

```text
dfsha/
├── cmd/
│   └── client/
├── internal/
│   └── client/
│       └── shell/
├── logs/
└── scripts/
    └── inspect_codebase.py
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
