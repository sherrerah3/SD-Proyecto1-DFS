package heartbeat

import (
	"context"
	"log"
)

// Aserción en tiempo de compilación: *LogReporter debe cumplir Reporter.
var _ Reporter = (*LogReporter)(nil)

// LogReporter registra los latidos en el log en vez de enviarlos por red.
//
// Es el Reporter por defecto mientras el Metanode (Persona 2) no publique el
// servicio gRPC de Heartbeat. Al estar detrás de la interfaz Reporter, cambiarlo
// por el cliente gRPC real no requiere tocar el Agent.
type LogReporter struct{}

func NewLogReporter() *LogReporter {
	return &LogReporter{}
}

func (r *LogReporter) Register(ctx context.Context, report Report) error {
	log.Printf(
		"heartbeat (log): registered datanode=%s addr=%s free=%d blocks=%d",
		report.DataNodeID, report.Address, report.FreeBytes, report.BlockCount,
	)

	return nil
}

func (r *LogReporter) Beat(ctx context.Context, report Report) ([]string, error) {
	log.Printf(
		"heartbeat (log): heartbeat datanode=%s free=%d blocks=%d",
		report.DataNodeID, report.FreeBytes, report.BlockCount,
	)

	return nil, nil
}
