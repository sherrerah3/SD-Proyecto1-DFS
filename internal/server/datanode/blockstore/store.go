package blockstore

import "io"

// BlockStore define el contrato de almacenamiento de bloques en disco,
// seguro para uso concurrente.
type BlockStore interface {
	// Write persiste un bloque leyendo de r y devuelve su hash y tamaño.
	Write(blockID string, r io.Reader) (BlockInfo, error)

	// Read abre un bloque para lectura en streaming; el llamador debe cerrarlo.
	Read(blockID string) (io.ReadCloser, BlockInfo, error)

	// Delete elimina un bloque del disco. Devuelve false si no existía.
	Delete(blockID string) (bool, error)

	// Verify recalcula el hash de un bloque desde disco.
	Verify(blockID string) (BlockReport, bool, error)

	// List devuelve el reporte de todos los bloques almacenados.
	List() ([]BlockReport, error)
}
