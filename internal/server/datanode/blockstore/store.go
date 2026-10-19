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

	// List returns a report for every stored block, including a freshly calculated hash.
	List() ([]BlockReport, error)

	// ListIDs returns stored block IDs without reading or hashing the block contents.
	ListIDs() ([]string, error)
}
