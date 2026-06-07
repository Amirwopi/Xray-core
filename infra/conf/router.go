package conf

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/xtls/xray-core/app/router"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/geodata"
	"github.com/xtls/xray-core/common/serial"
	feature_routing "github.com/xtls/xray-core/features/routing"

	"google.golang.org/protobuf/proto"
)

// StrategyConfig represents a strategy config
type StrategyConfig struct {
	Type     string           `json:"type"`
	Settings *json.RawMessage `json:"settings"`
}

type BalancingRule struct {
	Tag         string         `json:"tag"`
	Selectors   StringList     `json:"selector"`
	Strategy    StrategyConfig `json:"strategy"`
	FallbackTag string         `json:"fallbackTag"`
}

// Build builds the balancing rule
func (r *BalancingRule) Build() (*router.BalancingRule, error) {
	if r.Tag == "" {
		return nil, errors.New("empty balancer tag")
	}
	if len(r.Selectors) == 0 {
		return nil, errors.New("empty selector list")
	}

	r.Strategy.Type = strings.ToLower(r.Strategy.Type)
	switch r.Strategy.Type {
	case "":
		r.Strategy.Type = strategyRandom
	case strategyRandom, strategyLeastLoad, strategyLeastPing, strategyRoundRobin:
	default:
		return nil, errors.New("unknown balancing strategy: " + r.Strategy.Type)
	}

	settings := []byte("{}")
	if r.Strategy.Settings != nil {
		settings = ([]byte)(*r.Strategy.Settings)
	}
	rawConfig, err := strategyConfigLoader.LoadWithID(settings, r.Strategy.Type)
	if err != nil {
		return nil, errors.New("failed to parse to strategy config.").Base(err)
	}
	var ts proto.Message
	if builder, ok := rawConfig.(Buildable); ok {
		ts, err = builder.Build()
		if err != nil {
			return nil, err
		}
	}

	return &router.BalancingRule{
		Strategy:         r.Strategy.Type,
		StrategySettings: serial.ToTypedMessage(ts),
		FallbackTag:      r.FallbackTag,
		OutboundSelector: r.Selectors,
		Tag:              r.Tag,
	}, nil
}

type RouterConfig struct {
	RuleList       []json.RawMessage `json:"rules"`
	DomainStrategy *string           `json:"domainStrategy"`
	Balancers      []*BalancingRule  `json:"balancers"`
}

func (c *RouterConfig) getDomainStrategy() router.Config_DomainStrategy {
	ds := ""
	if c.DomainStrategy != nil {
		ds = *c.DomainStrategy
	}

	switch strings.ToLower(ds) {
	case "ipifnonmatch":
		return router.Config_IpIfNonMatch
	case "ipondemand":
		return router.Config_IpOnDemand
	default:
		return router.Config_AsIs
	}
}

func (c *RouterConfig) Build() (*router.Config, error) {
	config := new(router.Config)
	config.DomainStrategy = c.getDomainStrategy()

	var rawRuleList []json.RawMessage
	if c != nil {
		rawRuleList = c.RuleList
	}
	for _, rawRule := range rawRuleList {
		rule, err := parseRule(rawRule)
		if err != nil {
			return nil, err
		}
		config.Rule = append(config.Rule, rule)
	}

	for _, rawBalancer := range c.Balancers {
		balancer, err := rawBalancer.Build()
		if err != nil {
			return nil, err
		}
		config.BalancingRule = append(config.BalancingRule, balancer)
	}

	return config, nil
}

type RouterRule struct {
	RuleTag     string `json:"ruleTag"`
	OutboundTag string `json:"outboundTag"`
	BalancerTag string `json:"balancerTag"`
}

type WebhookRuleConfig struct {
	URL           string            `json:"url"`
	Deduplication uint32            `json:"deduplication"`
	Headers       map[string]string `json:"headers"`
}

func parseFieldRule(msg json.RawMessage) (*router.RoutingRule, error) {
	type RawFieldRule struct {
		RouterRule
		Domain         *StringList        `json:"domain"`
		Domains        *StringList        `json:"domains"`
		IP             *StringList        `json:"ip"`
		Port           *PortList          `json:"port"`
		Network        *NetworkList       `json:"network"`
		SourceIP       *StringList        `json:"sourceIP"`
		Source         *StringList        `json:"source"`
		SourcePort     *PortList          `json:"sourcePort"`
		User           *StringList        `json:"user"`
		VlessRoute     *PortList          `json:"vlessRoute"`
		InboundTag     *StringList        `json:"inboundTag"`
		Protocols      *StringList        `json:"protocol"`
		Attributes     map[string]string  `json:"attrs"`
		InboundHost    *StringList        `json:"inboundHost"`
		WSPath         *StringList        `json:"wsPath"`
		ServerName     *StringList        `json:"serverName"`
		CamouflageHost *StringList        `json:"camouflageHost"`
		LocalIP        *StringList        `json:"localIP"`
		LocalPort      *PortList          `json:"localPort"`
		Process        *StringList        `json:"process"`
		Webhook        *WebhookRuleConfig `json:"webhook"`
	}
	rawFieldRule := new(RawFieldRule)
	err := json.Unmarshal(msg, rawFieldRule)
	if err != nil {
		return nil, err
	}

	rule := new(router.RoutingRule)
	rule.RuleTag = rawFieldRule.RuleTag
	switch {
	case len(rawFieldRule.OutboundTag) > 0:
		rule.TargetTag = &router.RoutingRule_Tag{
			Tag: rawFieldRule.OutboundTag,
		}
	case len(rawFieldRule.BalancerTag) > 0:
		rule.TargetTag = &router.RoutingRule_BalancingTag{
			BalancingTag: rawFieldRule.BalancerTag,
		}
	default:
		return nil, errors.New("neither outboundTag nor balancerTag is specified in routing rule")
	}

	if rawFieldRule.Domain != nil {
		rules, err := geodata.ParseDomainRules(*rawFieldRule.Domain, geodata.Domain_Substr)
		if err != nil {
			return nil, err
		}
		rule.Domain = rules
	}

	if rawFieldRule.Domains != nil {
		rules, err := geodata.ParseDomainRules(*rawFieldRule.Domains, geodata.Domain_Substr)
		if err != nil {
			return nil, err
		}
		rule.Domain = rules
	}

	if rawFieldRule.IP != nil {
		rules, err := geodata.ParseIPRules(*rawFieldRule.IP)
		if err != nil {
			return nil, err
		}
		rule.Ip = rules
	}

	if rawFieldRule.Port != nil {
		rule.PortList = rawFieldRule.Port.Build()
	}

	if rawFieldRule.Network != nil {
		rule.Networks = rawFieldRule.Network.Build()
	}

	if rawFieldRule.SourceIP == nil {
		rawFieldRule.SourceIP = rawFieldRule.Source
	}

	if rawFieldRule.SourceIP != nil {
		rules, err := geodata.ParseIPRules(*rawFieldRule.SourceIP)
		if err != nil {
			return nil, err
		}
		rule.SourceIp = rules
	}

	if rawFieldRule.SourcePort != nil {
		rule.SourcePortList = rawFieldRule.SourcePort.Build()
	}

	if rawFieldRule.LocalIP != nil {
		rules, err := geodata.ParseIPRules(*rawFieldRule.LocalIP)
		if err != nil {
			return nil, err
		}
		rule.LocalIp = rules
	}

	if rawFieldRule.LocalPort != nil {
		rule.LocalPortList = rawFieldRule.LocalPort.Build()
	}

	if rawFieldRule.User != nil {
		for _, s := range *rawFieldRule.User {
			rule.UserEmail = append(rule.UserEmail, s)
		}
	}

	if rawFieldRule.VlessRoute != nil {
		rule.VlessRouteList = rawFieldRule.VlessRoute.Build()
	}

	if rawFieldRule.InboundTag != nil {
		for _, s := range *rawFieldRule.InboundTag {
			rule.InboundTag = append(rule.InboundTag, s)
		}
	}

	if rawFieldRule.Protocols != nil {
		for _, s := range *rawFieldRule.Protocols {
			rule.Protocol = append(rule.Protocol, s)
		}
	}

	if len(rawFieldRule.Attributes) > 0 {
		rule.Attributes = rawFieldRule.Attributes
	}
	mergeInboundAttributeRule(rule, feature_routing.AttrInboundHost, rawFieldRule.InboundHost, true)
	mergeInboundAttributeRule(rule, feature_routing.AttrInboundPath, rawFieldRule.WSPath, false)
	mergeInboundAttributeRule(rule, feature_routing.AttrInboundServerName, rawFieldRule.ServerName, true)
	mergeInboundAttributeRule(rule, feature_routing.AttrInboundCamouflageHost, rawFieldRule.CamouflageHost, true)

	if rawFieldRule.Process != nil && len(*rawFieldRule.Process) > 0 {
		rule.Process = *rawFieldRule.Process
	}

	if rawFieldRule.Webhook != nil && rawFieldRule.Webhook.URL != "" {
		rule.Webhook = &router.WebhookConfig{
			Url:           rawFieldRule.Webhook.URL,
			Deduplication: rawFieldRule.Webhook.Deduplication,
			Headers:       rawFieldRule.Webhook.Headers,
		}
	}

	return rule, nil
}

func mergeInboundAttributeRule(rule *router.RoutingRule, key string, values *StringList, normalizeLower bool) {
	if values == nil || len(*values) == 0 {
		return
	}
	if rule.Attributes == nil {
		rule.Attributes = make(map[string]string)
	}
	parts := make([]string, 0, len(*values))
	for _, value := range *values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if normalizeLower {
			value = strings.ToLower(value)
		}
		parts = append(parts, regexp.QuoteMeta(value))
	}
	if len(parts) == 0 {
		return
	}
	rule.Attributes[key] = "^(?:" + strings.Join(parts, "|") + ")$"
}

func parseRule(msg json.RawMessage) (*router.RoutingRule, error) {
	rawRule := new(RouterRule)
	err := json.Unmarshal(msg, rawRule)
	if err != nil {
		return nil, errors.New("invalid router rule").Base(err)
	}

	fieldrule, err := parseFieldRule(msg)
	if err != nil {
		return nil, errors.New("invalid field rule").Base(err)
	}
	return fieldrule, nil
}
