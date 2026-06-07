package router

import (
	"fmt"
	"strings"

	"github.com/xtls/xray-core/features/routing"
)

func routeDebugString(ctx routing.Context) string {
	attrs := ctx.GetAttributes()
	attr := func(key string) string {
		if attrs == nil {
			return ""
		}
		return attrs[strings.ToLower(key)]
	}
	return fmt.Sprintf(
		"inboundTag=%q user=%q targetDomain=%q sourcePort=%d targetPort=%d transport=%q host=%q path=%q serverName=%q camouflageHost=%q vlessRoute=%d",
		ctx.GetInboundTag(),
		ctx.GetUser(),
		ctx.GetTargetDomain(),
		ctx.GetSourcePort(),
		ctx.GetTargetPort(),
		attr(routing.AttrInboundTransport),
		attr(routing.AttrInboundHost),
		attr(routing.AttrInboundPath),
		attr(routing.AttrInboundServerName),
		attr(routing.AttrInboundCamouflageHost),
		ctx.GetVlessRoute(),
	)
}
