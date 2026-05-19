package service

import (
	"fmt"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/database"
	"github.com/mhsanaei/3x-ui/v3/database/model"
)

// naivePortBoundOn returns the listen value used by the naive server, normalised
// to "" when it means "all interfaces". Mirrors the rule we apply in the
// Caddyfile generator so the conflict check matches actual socket behaviour.
func naivePortBoundOn(srv *model.NaiveServer) string {
	l := strings.TrimSpace(srv.Listen)
	if l == "0.0.0.0" || l == "::" {
		return ""
	}
	return l
}

// naivePortInUse checks whether another naive server already binds the same
// (listen, port) pair. ignoreId>0 skips a specific row (for Update).
func naivePortInUse(port int, listen string, ignoreId int) (*model.NaiveServer, error) {
	db := database.GetDB()
	var rows []*model.NaiveServer
	q := db.Model(&model.NaiveServer{}).Where("port = ?", port)
	if ignoreId > 0 {
		q = q.Where("id != ?", ignoreId)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	wantListen := strings.TrimSpace(listen)
	if wantListen == "0.0.0.0" || wantListen == "::" {
		wantListen = ""
	}
	for _, r := range rows {
		if listenOverlaps(naivePortBoundOn(r), wantListen) {
			return r, nil
		}
	}
	return nil, nil
}

// xrayInboundOnPort returns the first xray inbound that binds the given (listen,
// port) pair, if any. The check considers only TCP transports because naive is
// always TCP (HTTPS). NodeID is ignored — naive servers run on the local panel
// only, so any inbound bound to the local node is a real collision; remote-node
// inbounds can't physically collide with our local listener and are skipped.
func xrayInboundOnPort(port int, listen string) (*model.Inbound, error) {
	db := database.GetDB()
	var rows []*model.Inbound
	q := db.Model(&model.Inbound{}).Where("port = ?", port)
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, in := range rows {
		if in.NodeID != nil {
			continue // remote node, can't collide with our local socket
		}
		if !listenOverlaps(in.Listen, listen) {
			continue
		}
		bits := inboundTransports(in.Protocol, in.StreamSettings, in.Settings)
		if bits&transportTCP != 0 {
			return in, nil
		}
	}
	return nil, nil
}

// crossCheckNaivePort surfaces a friendly error if the given naive config
// collides with an existing xray inbound or another naive row. Called from
// naive Add/Update before writing to the DB.
func crossCheckNaivePort(srv *model.NaiveServer, ignoreNaiveId int) error {
	if srv == nil || srv.Port <= 0 {
		return nil
	}
	if other, err := xrayInboundOnPort(srv.Port, srv.Listen); err != nil {
		return err
	} else if other != nil {
		label := other.Remark
		if label == "" {
			label = fmt.Sprintf("#%d", other.Id)
		}
		return fmt.Errorf("%w: port %d is already used by xray inbound %q", ErrNaiveInvalidConfig, srv.Port, label)
	}
	if other, err := naivePortInUse(srv.Port, srv.Listen, ignoreNaiveId); err != nil {
		return err
	} else if other != nil {
		label := other.Remark
		if label == "" {
			label = fmt.Sprintf("#%d", other.Id)
		}
		return fmt.Errorf("%w: port %d is already used by naive server %q", ErrNaiveInvalidConfig, srv.Port, label)
	}
	return nil
}

// naivePortConflictsInbound is the inverse: called by the xray inbound code path
// to ensure a new/edited inbound doesn't step on an existing naive server.
func naivePortConflictsInbound(inbound *model.Inbound) (*model.NaiveServer, error) {
	if inbound == nil || inbound.Port <= 0 || inbound.NodeID != nil {
		return nil, nil
	}
	bits := inboundTransports(inbound.Protocol, inbound.StreamSettings, inbound.Settings)
	if bits&transportTCP == 0 {
		return nil, nil
	}
	return naivePortInUse(inbound.Port, inbound.Listen, 0)
}
