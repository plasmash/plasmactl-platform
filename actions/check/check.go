package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/launchrctl/launchr/pkg/action"
	"github.com/plasmash/plasmactl-platform/pkg/graph"
)

// Finding represents a single validation finding.
type Finding struct {
	Section  string `json:"section"`
	Severity string `json:"severity"` // "error", "warning", "info"
	Message  string `json:"message"`
	Details  []string `json:"details,omitempty"`
}

// SectionResult holds results for one validation section.
type SectionResult struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Passed   int       `json:"passed"`
	Errors   int       `json:"errors"`
	Warnings int       `json:"warnings"`
	Findings []Finding `json:"findings"`
}

// CheckResult is the structured output for platform:check.
type CheckResult struct {
	Sections    []SectionResult `json:"sections"`
	TotalErrors   int           `json:"total_errors"`
	TotalWarnings int           `json:"total_warnings"`
}

// Check implements the platform:check command.
type Check struct {
	action.WithLogger
	action.WithTerm

	Section string

	result *CheckResult
}

// Result returns the structured result for JSON output.
func (c *Check) Result() any {
	return c.result
}

// Execute runs the platform:check action.
func (c *Check) Execute() error {
	g, err := graph.Load()
	if err != nil {
		return fmt.Errorf("failed to load graph: %w", err)
	}

	c.result = &CheckResult{}

	sections := map[string]func(*graph.PlatformGraph) SectionResult{
		"components": c.checkComponents,
		"model":      c.checkModel,
		"chassis":    c.checkChassis,
		"nodes":      c.checkNodes,
		"variables":  c.checkVariables,
		"flows":      c.checkFlows,
	}

	order := []string{"components", "model", "chassis", "nodes", "variables", "flows"}

	for _, name := range order {
		if c.Section != "" && c.Section != name {
			continue
		}
		sr := sections[name](g)
		c.result.Sections = append(c.result.Sections, sr)
		c.result.TotalErrors += sr.Errors
		c.result.TotalWarnings += sr.Warnings
	}

	if c.result.TotalErrors > 0 {
		return fmt.Errorf("check found %d error(s)", c.result.TotalErrors)
	}

	return nil
}

// checkComponents validates component integrity.
func (c *Check) checkComponents(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "components", Name: "Component Integrity"}

	// 1. Circular dependency detection
	depEdges := graph.ComponentDependencyEdgeTypes()
	cycles := g.FindCycles(depEdges...)
	if len(cycles) == 0 {
		sr.Passed++
	} else {
		details := make([]string, 0, len(cycles))
		for _, cycle := range cycles {
			details = append(details, strings.Join(cycle, " → ")+" → "+cycle[0])
		}
		sort.Strings(details)
		sr.Findings = append(sr.Findings, Finding{
			Section:  "components",
			Severity: "error",
			Message:  fmt.Sprintf("%d circular dependency chain(s)", len(cycles)),
			Details:  details,
		})
		sr.Errors++
	}

	// 2. Orphan components: components not attached to any chassis and not required by any attached component
	components := g.NodesByType("component")
	attachedComponents := make(map[string]bool)
	for _, comp := range components {
		edges := g.EdgesTo(comp.Name, "attaches")
		if len(edges) > 0 {
			attachedComponents[comp.Name] = true
		}
	}

	// Find components reachable from attached ones via dependency edges
	reachable := make(map[string]bool)
	for name := range attachedComponents {
		reachable[name] = true
		for _, desc := range g.Descendants(name, 0, depEdges...) {
			if desc.Type == "component" {
				reachable[desc.Name] = true
			}
		}
	}

	var orphans []string
	for _, comp := range components {
		if !reachable[comp.Name] {
			orphans = append(orphans, comp.Name)
		}
	}
	sort.Strings(orphans)
	if len(orphans) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "components",
			Severity: "warning",
			Message:  fmt.Sprintf("%d orphan component(s) (not attached, not required)", len(orphans)),
			Details:  orphans,
		})
		sr.Warnings++
	}

	// 3. Missing dependencies: dependency edges pointing to nodes that don't exist as proper components
	var missing []string
	for _, e := range g.AllEdges() {
		if !graph.IsDependencyEdge(e.Type) {
			continue
		}
		target := g.Node(e.To().Name)
		if target == nil {
			continue
		}
		// If target was auto-created (has no path and no version), it's likely missing
		if target.Type == "component" && target.Version == "" && target.Path == "" {
			missing = append(missing, fmt.Sprintf("%s requires %s (not in composition)", e.From().Name, e.To().Name))
		}
	}
	sort.Strings(missing)
	missing = dedupStrings(missing)
	if len(missing) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "components",
			Severity: "error",
			Message:  fmt.Sprintf("%d missing dependency(ies)", len(missing)),
			Details:  missing,
		})
		sr.Errors++
	}

	return sr
}

// checkModel validates model/package completeness.
func (c *Check) checkModel(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "model", Name: "Model Completeness"}

	packages := g.NodesByType("package")
	depEdges := graph.ComponentDependencyEdgeTypes()

	// 1. Cross-package dependency completeness
	// For each package, check if components it contains depend on components from packages
	// that aren't in the composition
	var crossPkgIssues []string
	pkgComponents := make(map[string]map[string]bool) // pkg → set of component names
	componentPkg := make(map[string]string)            // component → pkg

	for _, pkg := range packages {
		comps := make(map[string]bool)
		for _, e := range g.EdgesFrom(pkg.Name, "contains") {
			if e.To().Type == "component" {
				comps[e.To().Name] = true
				componentPkg[e.To().Name] = pkg.Name
			}
		}
		pkgComponents[pkg.Name] = comps
	}

	for _, pkg := range packages {
		for compName := range pkgComponents[pkg.Name] {
			for _, e := range g.EdgesFrom(compName, depEdges...) {
				targetComp := e.To().Name
				if e.To().Type != "component" {
					continue
				}
				targetPkg := componentPkg[targetComp]
				if targetPkg == "" && e.To().Version == "" && e.To().Path == "" {
					crossPkgIssues = append(crossPkgIssues,
						fmt.Sprintf("%s (in %s) requires %s (not in any package)", compName, pkg.Name, targetComp))
				}
			}
		}
	}
	sort.Strings(crossPkgIssues)
	crossPkgIssues = dedupStrings(crossPkgIssues)
	if len(crossPkgIssues) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "model",
			Severity: "error",
			Message:  fmt.Sprintf("%d cross-package dependency issue(s)", len(crossPkgIssues)),
			Details:  crossPkgIssues,
		})
		sr.Errors++
	}

	// 2. Duplicate components: same component in multiple packages
	compPackages := make(map[string][]string) // component → list of packages
	for _, pkg := range packages {
		for compName := range pkgComponents[pkg.Name] {
			compPackages[compName] = append(compPackages[compName], pkg.Name)
		}
	}
	var duplicates []string
	for comp, pkgs := range compPackages {
		if len(pkgs) > 1 {
			sort.Strings(pkgs)
			duplicates = append(duplicates, fmt.Sprintf("%s (in %s)", comp, strings.Join(pkgs, ", ")))
		}
	}
	sort.Strings(duplicates)
	if len(duplicates) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "model",
			Severity: "error",
			Message:  fmt.Sprintf("%d duplicate component(s) across packages", len(duplicates)),
			Details:  duplicates,
		})
		sr.Errors++
	}

	// 3. Unused packages: packages whose components are all unreachable from attached components
	attachedComponents := make(map[string]bool)
	for _, comp := range g.NodesByType("component") {
		if edges := g.EdgesTo(comp.Name, "attaches"); len(edges) > 0 {
			attachedComponents[comp.Name] = true
		}
	}
	reachable := make(map[string]bool)
	for name := range attachedComponents {
		reachable[name] = true
		for _, desc := range g.Descendants(name, 0, depEdges...) {
			if desc.Type == "component" {
				reachable[desc.Name] = true
			}
		}
	}

	var unusedPkgs []string
	for _, pkg := range packages {
		hasReachable := false
		for compName := range pkgComponents[pkg.Name] {
			if reachable[compName] {
				hasReachable = true
				break
			}
		}
		if !hasReachable && len(pkgComponents[pkg.Name]) > 0 {
			unusedPkgs = append(unusedPkgs, fmt.Sprintf("%s (%d components, 0 reachable)", pkg.Name, len(pkgComponents[pkg.Name])))
		}
	}
	sort.Strings(unusedPkgs)
	if len(unusedPkgs) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "model",
			Severity: "warning",
			Message:  fmt.Sprintf("%d unused package(s)", len(unusedPkgs)),
			Details:  unusedPkgs,
		})
		sr.Warnings++
	}

	return sr
}

// checkChassis validates chassis coverage.
func (c *Check) checkChassis(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "chassis", Name: "Chassis Coverage"}

	chassisNodes := g.NodesByKind("chassis")

	// 1. Empty chassis paths: paths with zero node allocations
	var emptyPaths []string
	for _, ch := range chassisNodes {
		nodes := g.EdgesTo(ch.Name, "memberof")
		if len(nodes) == 0 {
			emptyPaths = append(emptyPaths, ch.Name)
		}
	}
	sort.Strings(emptyPaths)
	if len(emptyPaths) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "chassis",
			Severity: "error",
			Message:  fmt.Sprintf("%d chassis path(s) with zero nodes", len(emptyPaths)),
			Details:  emptyPaths,
		})
		sr.Errors++
	}

	// 2. Unattached paths: paths with zero component attachments
	var unattachedPaths []string
	for _, ch := range chassisNodes {
		attachments := g.EdgesFrom(ch.Name, "attaches")
		if len(attachments) == 0 {
			unattachedPaths = append(unattachedPaths, ch.Name)
		}
	}
	sort.Strings(unattachedPaths)
	if len(unattachedPaths) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "chassis",
			Severity: "warning",
			Message:  fmt.Sprintf("%d chassis path(s) with zero component attachments", len(unattachedPaths)),
			Details:  unattachedPaths,
		})
		sr.Warnings++
	}

	// 3. Single-node chassis paths (no redundancy)
	var singleNode []string
	for _, ch := range chassisNodes {
		nodes := g.EdgesTo(ch.Name, "memberof")
		attachments := g.EdgesFrom(ch.Name, "attaches")
		if len(nodes) == 1 && len(attachments) > 0 {
			singleNode = append(singleNode, fmt.Sprintf("%s (%d component(s), 1 node)", ch.Name, len(attachments)))
		}
	}
	sort.Strings(singleNode)
	if len(singleNode) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "chassis",
			Severity: "warning",
			Message:  fmt.Sprintf("%d single-node chassis path(s) (no redundancy)", len(singleNode)),
			Details:  singleNode,
		})
		sr.Warnings++
	}

	return sr
}

// checkNodes validates node coverage and infrastructure health.
func (c *Check) checkNodes(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "nodes", Name: "Node Coverage"}

	infraNodes := g.NodesByKind("node")

	// 1. Nodes with no chassis allocations
	var unallocated []string
	for _, n := range infraNodes {
		allocations := g.EdgesFrom(n.Name, "memberof")
		if len(allocations) == 0 {
			unallocated = append(unallocated, n.Name)
		}
	}
	sort.Strings(unallocated)
	if len(unallocated) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "nodes",
			Severity: "warning",
			Message:  fmt.Sprintf("%d node(s) with no chassis allocations", len(unallocated)),
			Details:  unallocated,
		})
		sr.Warnings++
	}

	// 2. Single points of failure: chassis paths served by exactly one node
	//    and that node is the only one serving components
	chassisNodes := g.NodesByKind("chassis")
	var spof []string
	for _, ch := range chassisNodes {
		members := g.EdgesTo(ch.Name, "memberof")
		attachments := g.EdgesFrom(ch.Name, "attaches")
		if len(members) == 1 && len(attachments) > 0 {
			spof = append(spof, fmt.Sprintf("%s → sole node: %s (%d component(s))",
				ch.Name, members[0].From().Name, len(attachments)))
		}
	}
	sort.Strings(spof)
	if len(spof) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "nodes",
			Severity: "warning",
			Message:  fmt.Sprintf("%d single point(s) of failure", len(spof)),
			Details:  spof,
		})
		sr.Warnings++
	}

	return sr
}

// checkVariables validates variable health.
func (c *Check) checkVariables(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "variables", Name: "Variable Health"}

	variables := g.NodesByType("variable")

	var undefined []string
	var orphanVars []string
	var duplicateDefs []string

	for _, v := range variables {
		defines := g.EdgesTo(v.Name, "defines")
		overrides := g.EdgesTo(v.Name, "overrides")
		parametrizes := g.EdgesFrom(v.Name, "parametrizes")

		hasProducer := len(defines) > 0 || len(overrides) > 0
		hasConsumer := len(parametrizes) > 0

		// Undefined: consumed but never defined
		if !hasProducer && hasConsumer {
			consumers := make([]string, 0, len(parametrizes))
			for _, e := range parametrizes {
				consumers = append(consumers, e.To().Name)
			}
			sort.Strings(consumers)
			if len(consumers) > 3 {
				undefined = append(undefined, fmt.Sprintf("%s (used by %d components)", v.Name, len(consumers)))
			} else {
				undefined = append(undefined, fmt.Sprintf("%s (used by %s)", v.Name, strings.Join(consumers, ", ")))
			}
		}

		// Orphans: defined but never consumed
		if hasProducer && !hasConsumer {
			orphanVars = append(orphanVars, v.Name)
		}

		// Duplicate definitions: multiple defines edges from different components
		if len(defines) > 1 {
			sources := make([]string, 0, len(defines))
			for _, e := range defines {
				sources = append(sources, e.From().Name)
			}
			sort.Strings(sources)
			duplicateDefs = append(duplicateDefs, fmt.Sprintf("%s (defined in %s)", v.Name, strings.Join(sources, ", ")))
		}
	}

	sort.Strings(undefined)
	if len(undefined) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "variables",
			Severity: "error",
			Message:  fmt.Sprintf("%d undefined variable(s) (used but never defined)", len(undefined)),
			Details:  undefined,
		})
		sr.Errors++
	}

	sort.Strings(orphanVars)
	if len(orphanVars) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "variables",
			Severity: "warning",
			Message:  fmt.Sprintf("%d orphan variable(s) (defined but never used)", len(orphanVars)),
			Details:  orphanVars,
		})
		sr.Warnings++
	}

	sort.Strings(duplicateDefs)
	if len(duplicateDefs) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "variables",
			Severity: "error",
			Message:  fmt.Sprintf("%d duplicate variable definition(s)", len(duplicateDefs)),
			Details:  duplicateDefs,
		})
		sr.Errors++
	}

	return sr
}

// checkFlows validates the agent→skill→function orchestration chain.
func (c *Check) checkFlows(g *graph.PlatformGraph) SectionResult {
	sr := SectionResult{ID: "flows", Name: "Flow Orchestration"}

	agents := g.NodesByKind("agent")
	skills := g.NodesByKind("skill")
	functions := g.NodesByKind("function")

	if len(agents) == 0 && len(skills) == 0 {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "flows",
			Severity: "info",
			Message:  "No agents or skills in graph",
		})
		return sr
	}

	// Separate flows from executors: executors group flows, flows orchestrate skills.
	var flowAgents []*graph.PlatformNode
	for _, a := range agents {
		if !strings.Contains(a.Name, ".executors.") {
			flowAgents = append(flowAgents, a)
		}
	}

	// 1. Flows without skills: flows that don't orchestrate any skill
	var flowsNoSkill []string
	for _, f := range flowAgents {
		edges := g.EdgesFrom(f.Name, "orchestrates")
		hasSkill := false
		for _, e := range edges {
			if e.To().Kind == "skill" {
				hasSkill = true
				break
			}
		}
		if !hasSkill {
			flowsNoSkill = append(flowsNoSkill, f.Name)
		}
	}
	sort.Strings(flowsNoSkill)
	if len(flowsNoSkill) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "flows",
			Severity: "warning",
			Message:  fmt.Sprintf("%d flow(s) without skills", len(flowsNoSkill)),
			Details:  flowsNoSkill,
		})
		sr.Warnings++
	}

	// 2. Skills without functions: skills that don't configure any function
	functionSet := make(map[string]bool)
	for _, f := range functions {
		functionSet[f.Name] = true
	}
	var skillsNoFunc []string
	for _, s := range skills {
		edges := g.EdgesFrom(s.Name, "configures")
		hasFunc := false
		for _, e := range edges {
			if functionSet[e.To().Name] {
				hasFunc = true
				break
			}
		}
		if !hasFunc {
			skillsNoFunc = append(skillsNoFunc, s.Name)
		}
	}
	sort.Strings(skillsNoFunc)
	if len(skillsNoFunc) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "flows",
			Severity: "warning",
			Message:  fmt.Sprintf("%d skill(s) not configuring any function", len(skillsNoFunc)),
			Details:  skillsNoFunc,
		})
		sr.Warnings++
	}

	// 3. Orphan skills: skills not orchestrated by any agent
	orchestratedSkills := make(map[string]bool)
	for _, a := range agents {
		for _, e := range g.EdgesFrom(a.Name, "orchestrates") {
			if e.To().Kind == "skill" {
				orchestratedSkills[e.To().Name] = true
			}
		}
	}
	var orphanSkills []string
	for _, s := range skills {
		if !orchestratedSkills[s.Name] {
			orphanSkills = append(orphanSkills, s.Name)
		}
	}
	sort.Strings(orphanSkills)
	if len(orphanSkills) == 0 {
		sr.Passed++
	} else {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "flows",
			Severity: "warning",
			Message:  fmt.Sprintf("%d orphan skill(s) (not orchestrated by any agent)", len(orphanSkills)),
			Details:  orphanSkills,
		})
		sr.Warnings++
	}

	// 4. Choreography: flow-to-flow connections via trigger/output channels.
	// When choreographs edges exist, validate for cycles and dead-ends.
	hasChoreographs := false
	for _, e := range g.AllEdges() {
		if e.Type == "choreographs" {
			hasChoreographs = true
			break
		}
	}
	if !hasChoreographs {
		sr.Findings = append(sr.Findings, Finding{
			Section:  "flows",
			Severity: "info",
			Message:  "No choreography edges in graph (trigger/output channel analysis pending)",
		})
	} else {
		cycles := g.FindCycles("choreographs")
		if len(cycles) == 0 {
			sr.Passed++
		} else {
			details := make([]string, 0, len(cycles))
			for _, cycle := range cycles {
				details = append(details, strings.Join(cycle, " → ")+" → "+cycle[0])
			}
			sort.Strings(details)
			sr.Findings = append(sr.Findings, Finding{
				Section:  "flows",
				Severity: "error",
				Message:  fmt.Sprintf("%d choreography cycle(s)", len(cycles)),
				Details:  details,
			})
			sr.Errors++
		}
	}

	return sr
}

// PrintText outputs human-readable validation results.
func (c *Check) PrintText() {
	for _, sr := range c.result.Sections {
		c.Term().Info().Println(sr.Name)

		for _, f := range sr.Findings {
			switch f.Severity {
			case "error":
				c.Term().Error().Printfln("  ✗ %s", f.Message)
			case "warning":
				c.Term().Warning().Printfln("  ⚠ %s", f.Message)
			case "info":
				c.Term().Info().Printfln("  - %s", f.Message)
			}
			for _, d := range f.Details {
				c.Term().Printfln("    - %s", d)
			}
		}

		if sr.Passed > 0 && sr.Errors == 0 && sr.Warnings == 0 {
			c.Term().Success().Printfln("  ✓ All %d check(s) passed", sr.Passed)
		} else if sr.Passed > 0 {
			c.Term().Success().Printfln("  ✓ %d check(s) passed", sr.Passed)
		}

		c.Term().Info().Println()
	}
}

// dedupStrings removes duplicate entries from a sorted slice.
func dedupStrings(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	result := []string{s[0]}
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1] {
			result = append(result, s[i])
		}
	}
	return result
}
