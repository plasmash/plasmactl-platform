import React, { useState, useRef, useEffect, useMemo, useCallback } from 'react'
import {
  Box,
  Button,
  TextField,
  Typography,
  Tabs,
  Tab,
  Chip,
  Alert,
  FormControlLabel,
  Switch,
  IconButton,
  Tooltip,
  Stack,
  Paper,
  useTheme,
} from '@mui/material'
import { DataGrid } from '@mui/x-data-grid'
import PlayArrow from '@mui/icons-material/PlayArrow'
import ZoomIn from '@mui/icons-material/ZoomIn'
import ZoomOut from '@mui/icons-material/ZoomOut'
import CenterFocusStrong from '@mui/icons-material/CenterFocusStrong'
import Refresh from '@mui/icons-material/Refresh'
import cytoscape from 'cytoscape'

// Color palette for node kinds (component subtypes).
const kindColors: Record<string, string> = {
  application: '#2980B9',
  service:     '#3498DB',
  software:    '#8E44AD',
  agent:       '#27AE60',
  skill:       '#2ECC71',
  function:    '#1ABC9C',
  library:     '#F39C12',
  entity:      '#E74C3C',
  metric:      '#E91E63',
  builder:     '#7F8C8D',
  helper:      '#95A5A6',
  zone:        '#795548',
  variable:    '#78909C',
  node:        '#546E7A',
  package:     '#E67E22',
  model:       '#9B59B6',
  platform:    '#4A90D9',
}

// Color palette for structural node types (fallback when kind is empty).
const typeColors: Record<string, string> = {
  component: '#78909C',
  zone:      '#795548',
  variable:  '#78909C',
  node:      '#546E7A',
  package:   '#E67E22',
  model:     '#9B59B6',
  platform:  '#4A90D9',
}

// Color palette for edge types (aligned with types.rs:determine_edge_type).
const edgeColors: Record<string, string> = {
  depends:       '#78909C',
  orchestrates:  '#7B1FA2',
  configures:    '#0288D1',
  computes:      '#00897B',
  implements:    '#1565C0',
  uses:          '#455A64',
  builds:        '#F57C00',
  contains:      '#9E9E9E',
  executes:      '#9C27B0',
  materializes:  '#2E7D32',
  defines:       '#1976D2',
  parametrizes:  '#6A1B9A',
  allocates:     '#5D4037',
  overrides:     '#FF6F00',
  distributes:   '#D32F2F',
  choreographs:  '#00BCD4',
  // Legacy/fallback
  requires:      '#1976d2',
  composes:      '#ed6c02',
  attaches:      '#d32f2f',
  hosts:         '#455a64',
  provides:      '#00796b',
}

// Edge dash patterns by type.
const dashedEdges = new Set(['uses', 'builds', 'overrides', 'allocates'])
const dottedEdges = new Set(['contains', 'parametrizes'])

const defaultColor = '#757575'

function getNodeColor(node: { kind?: string; type: string }): string {
  if (node.kind && kindColors[node.kind]) return kindColors[node.kind]
  return typeColors[node.type] || defaultColor
}

function getKindColor(kind: string): string {
  return kindColors[kind] || defaultColor
}

function getTypeColor(type: string): string {
  return typeColors[type] || defaultColor
}

function getEdgeColor(type: string): string {
  return edgeColors[type] || defaultColor
}

// Extract short label from dotted ID: "interaction.applications.dashboards" -> "dashboards"
function shortLabel(id: string): string {
  const parts = id.split('.')
  return parts[parts.length - 1]
}

const LARGE_GRAPH_THRESHOLD = 500

interface GraphNode {
  id: string
  type: string
  kind?: string
  layer?: string
}

interface GraphEdge {
  source: string
  target: string
  type: string
}

interface GraphResult {
  query: { node: string; reverse: boolean; depth: number }
  nodes: GraphNode[]
  edges: GraphEdge[]
  stats: { total_nodes: number; total_edges: number }
}

interface HoverInfo {
  id: string
  kind: string
  layer: string
  x: number
  y: number
}

export default function GraphUI({ action, execute, isRunning, runInfo, streams, displayMode, setDisplayMode, DefaultOutput }: any) {
  const theme = useTheme()
  const isDark = theme.palette.mode === 'dark'

  const [query, setQuery] = useState('')
  const [reverse, setReverse] = useState(false)
  const [depth, setDepth] = useState(2)
  const [depthStr, setDepthStr] = useState('2')
  const [tab, setTab] = useState(2)
  const [showLargeGraph, setShowLargeGraph] = useState(false)
  const [largeGraphDismissed, setLargeGraphDismissed] = useState(false)
  const [hiddenNodeTypes, setHiddenNodeTypes] = useState<Set<string>>(new Set())
  const [hiddenEdgeTypes, setHiddenEdgeTypes] = useState<Set<string>>(new Set())
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set())
  const [hideIsolated, setHideIsolated] = useState(false)
  const [hoverInfo, setHoverInfo] = useState<HoverInfo | null>(null)

  const cyRef = useRef<HTMLDivElement>(null)
  const cyInstance = useRef<any>(null)

  const result: GraphResult | null = runInfo?.result ?? null

  const handleExecute = useCallback((q?: string) => {
    const queryValue = q !== undefined ? q : query
    setLargeGraphDismissed(false)
    setHiddenNodeTypes(new Set())
    setHiddenEdgeTypes(new Set())
    setHiddenKinds(new Set())
    setHideIsolated(false)
    setDisplayMode('main')
    execute({
      arguments: { query: queryValue },
      options: { reverse, depth, tree: false, format: 'json', debug: false },
    })
  }, [query, reverse, depth, execute, setDisplayMode])

  const handleNodeClick = useCallback((nodeId: string) => {
    setQuery(nodeId)
    setLargeGraphDismissed(false)
    setHiddenNodeTypes(new Set())
    setHiddenEdgeTypes(new Set())
    setHiddenKinds(new Set())
    setHideIsolated(false)
    setDisplayMode('main')
    execute({
      arguments: { query: nodeId },
      options: { reverse, depth, tree: false, format: 'json', debug: false },
    })
  }, [reverse, depth, execute, setDisplayMode])

  // Convert result to cytoscape elements.
  const cyElements = useMemo(() => {
    if (!result?.nodes) return []
    const nodes = result.nodes.map((n) => ({
      data: {
        id: n.id,
        label: shortLabel(n.id),
        fullId: n.id,
        type: n.type,
        kind: n.kind || '',
        layer: n.layer || '',
      },
    }))
    const edges = (result.edges || []).map((e, i) => ({
      data: {
        id: `e${i}`,
        source: e.source,
        target: e.target,
        type: e.type,
      },
    }))
    return [...nodes, ...edges]
  }, [result])

  // Collect unique kinds for legend.
  const nodeKinds = useMemo(() => {
    if (!result?.nodes) return []
    return [...new Set(result.nodes.map((n) => n.kind).filter(Boolean))].sort() as string[]
  }, [result])

  // Collect unique types for legend.
  const nodeTypes = useMemo(() => {
    if (!result?.nodes) return []
    return [...new Set(result.nodes.map((n) => n.type))].sort()
  }, [result])

  const edgeTypes = useMemo(() => {
    if (!result?.edges) return []
    return [...new Set(result.edges.map((e) => e.type))].sort()
  }, [result])

  const textOutlineColor = isDark ? '#111' : '#333'

  // Cytoscape stylesheet.
  const cyStylesheet = useMemo(() => {
    // Kind-based node colors.
    const kindStyles = Object.entries(kindColors).map(([kind, color]) => ({
      selector: `node[kind="${kind}"]`,
      style: {
        'background-color': color,
      },
    }))
    // Type-based node colors (fallback for nodes without kind).
    const nodeTypeStyles = Object.entries(typeColors).map(([type, color]) => ({
      selector: `node[type="${type}"][kind=""]`,
      style: {
        'background-color': color,
      },
    }))
    // Edge colors.
    const edgeStyles = Object.entries(edgeColors).map(([type, color]) => ({
      selector: `edge[type="${type}"]`,
      style: {
        'line-color': color,
        'target-arrow-color': color,
      },
    }))
    // Edge dash patterns.
    const dashedStyles = [...dashedEdges].map((type) => ({
      selector: `edge[type="${type}"]`,
      style: { 'line-style': 'dashed' as const },
    }))
    const dottedStyles = [...dottedEdges].map((type) => ({
      selector: `edge[type="${type}"]`,
      style: { 'line-style': 'dotted' as const },
    }))
    return [
      {
        selector: 'node',
        style: {
          'background-color': defaultColor,
          label: 'data(label)',
          color: '#fff',
          'text-outline-color': textOutlineColor,
          'text-outline-width': 1,
          'font-size': '10px',
          'text-valign': 'center' as const,
          'text-halign': 'center' as const,
          width: 30,
          height: 30,
        },
      },
      {
        selector: 'edge',
        style: {
          width: 1.5,
          'line-color': defaultColor,
          'target-arrow-color': defaultColor,
          'target-arrow-shape': 'triangle' as const,
          'curve-style': 'bezier' as const,
          'arrow-scale': 0.8,
        },
      },
      {
        selector: 'node:selected',
        style: {
          'border-width': 3,
          'border-color': '#ffc107',
        },
      },
      {
        selector: 'node:active',
        style: {
          'overlay-opacity': 0.1,
        },
      },
      // Node shapes by type.
      {
        selector: 'node[type="variable"]',
        style: { shape: 'diamond' as const, width: 25, height: 25 },
      },
      {
        selector: 'node[type="zone"]',
        style: { shape: 'round-rectangle' as const },
      },
      {
        selector: 'node[type="node"]',
        style: { shape: 'hexagon' as const },
      },
      {
        selector: 'node[type="package"]',
        style: { shape: 'barrel' as const },
      },
      // Hover highlighting.
      {
        selector: 'node.hovered',
        style: {
          'border-width': 3,
          'border-color': '#ffc107',
          'z-index': 10,
        },
      },
      {
        selector: 'node.hovered-neighbor',
        style: {
          'border-width': 2,
          'border-color': '#ffc107',
          'z-index': 9,
          opacity: 1,
        },
      },
      {
        selector: 'node.dimmed',
        style: {
          opacity: 0.15,
        },
      },
      {
        selector: 'edge.dimmed',
        style: {
          opacity: 0.08,
        },
      },
      {
        selector: 'edge.hovered-neighbor',
        style: {
          opacity: 1,
          width: 2.5,
          'z-index': 9,
        },
      },
      ...kindStyles,
      ...nodeTypeStyles,
      ...edgeStyles,
      ...dashedStyles,
      ...dottedStyles,
    ]
  }, [textOutlineColor])

  // Build layout options.
  const getLayoutOptions = useCallback((totalNodes: number) => {
    if (totalNodes <= 50) {
      return {
        name: 'concentric',
        animate: false,
        concentric: (node: any) => node.degree(),
        levelWidth: () => 3,
        minNodeSpacing: 30,
      }
    }
    if (totalNodes <= 300) {
      return {
        name: 'cose',
        animate: false,
        randomize: true,
        nodeRepulsion: () => 80000,
        idealEdgeLength: () => 150,
        gravity: 0.03,
        numIter: 500,
      }
    }
    return {
      name: 'cose',
      animate: false,
      randomize: true,
      nodeRepulsion: () => 50000,
      idealEdgeLength: () => 120,
      gravity: 0.05,
      numIter: 200,
    }
  }, [])

  // Re-layout handler.
  const handleRelayout = useCallback(() => {
    const cy = cyInstance.current
    if (!cy) return
    const visibleNodes = cy.nodes(':visible').length
    cy.layout(getLayoutOptions(visibleNodes)).run()
  }, [getLayoutOptions])

  // Initialize / update cytoscape when tab=2 and elements change.
  useEffect(() => {
    if (tab !== 2 || !cyRef.current || cyElements.length === 0) return

    // Check large graph warning.
    const nodeCount = result?.stats?.total_nodes ?? 0
    if (nodeCount > LARGE_GRAPH_THRESHOLD && !largeGraphDismissed) {
      setShowLargeGraph(true)
      return
    }

    // Destroy previous instance.
    if (cyInstance.current) {
      cyInstance.current.destroy()
      cyInstance.current = null
    }

    const totalNodes = result?.stats?.total_nodes ?? 0

    const cy = cytoscape({
      container: cyRef.current,
      elements: cyElements,
      style: cyStylesheet,
      minZoom: 0.1,
      maxZoom: 5,
      wheelSensitivity: 0.3,
    })

    // Run layout.
    cy.layout(getLayoutOptions(totalNodes)).run()

    // Click handler: re-query on node click.
    cy.on('tap', 'node', (evt: any) => {
      const nodeId = evt.target.data('fullId')
      if (nodeId) handleNodeClick(nodeId)
    })

    // Hover highlighting.
    cy.on('mouseover', 'node', (evt: any) => {
      const node = evt.target
      const neighborhood = node.neighborhood().add(node)
      cy.elements().not(neighborhood).addClass('dimmed')
      node.addClass('hovered')
      neighborhood.not(node).addClass('hovered-neighbor')
      neighborhood.edges().addClass('hovered-neighbor')

      // Show tooltip.
      const d = node.data()
      const pos = node.renderedPosition()
      setHoverInfo({
        id: d.fullId,
        kind: d.kind || d.type,
        layer: d.layer || '-',
        x: pos.x,
        y: pos.y,
      })
    })

    cy.on('mouseout', 'node', () => {
      cy.elements().removeClass('dimmed hovered hovered-neighbor')
      setHoverInfo(null)
    })

    cyInstance.current = cy
    return () => {
      if (cyInstance.current) {
        cyInstance.current.destroy()
        cyInstance.current = null
      }
    }
  }, [tab, cyElements, cyStylesheet, largeGraphDismissed, result, handleNodeClick, getLayoutOptions])

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      if (cyInstance.current) {
        cyInstance.current.destroy()
        cyInstance.current = null
      }
    }
  }, [])

  // Filter toggles.
  const toggleNodeType = useCallback((type: string) => {
    setHiddenNodeTypes((prev) => {
      const next = new Set(prev)
      if (next.has(type)) next.delete(type)
      else next.add(type)
      return next
    })
  }, [])

  const toggleEdgeType = useCallback((type: string) => {
    setHiddenEdgeTypes((prev) => {
      const next = new Set(prev)
      if (next.has(type)) next.delete(type)
      else next.add(type)
      return next
    })
  }, [])

  const toggleKind = useCallback((kind: string) => {
    setHiddenKinds((prev) => {
      const next = new Set(prev)
      if (next.has(kind)) next.delete(kind)
      else next.add(kind)
      return next
    })
  }, [])

  // Apply filter visibility to cytoscape elements.
  useEffect(() => {
    const cy = cyInstance.current
    if (!cy) return
    cy.batch(() => {
      // First pass: determine node visibility from type and kind filters.
      cy.nodes().forEach((node: any) => {
        const nodeType = node.data('type')
        const nodeKind = node.data('kind')
        const typeHidden = hiddenNodeTypes.has(nodeType)
        const kindHidden = nodeKind && hiddenKinds.has(nodeKind)
        if (typeHidden || kindHidden) node.hide()
        else node.show()
      })

      // Edge visibility: hidden if edge type hidden or either endpoint hidden.
      cy.edges().forEach((edge: any) => {
        const src = cy.getElementById(edge.data('source'))
        const tgt = cy.getElementById(edge.data('target'))
        const srcHidden = !src.visible()
        const tgtHidden = !tgt.visible()
        if (hiddenEdgeTypes.has(edge.data('type')) || srcHidden || tgtHidden) edge.hide()
        else edge.show()
      })

      // Second pass: hide isolated nodes (nodes with no visible edges).
      if (hideIsolated) {
        cy.nodes(':visible').forEach((node: any) => {
          const visibleEdges = node.connectedEdges(':visible')
          if (visibleEdges.length === 0) node.hide()
        })
      }
    })
  }, [hiddenNodeTypes, hiddenEdgeTypes, hiddenKinds, hideIsolated])

  // Zoom controls.
  const handleZoomIn = () => cyInstance.current?.zoom(cyInstance.current.zoom() * 1.3)
  const handleZoomOut = () => cyInstance.current?.zoom(cyInstance.current.zoom() / 1.3)
  const handleFit = () => cyInstance.current?.fit(undefined, 30)

  // DataGrid columns.
  const nodeColumns = [
    { field: 'id', headerName: 'ID', flex: 2, minWidth: 200 },
    { field: 'type', headerName: 'Type', flex: 1, minWidth: 100 },
    { field: 'kind', headerName: 'Kind', flex: 1, minWidth: 100 },
    { field: 'layer', headerName: 'Layer', flex: 1, minWidth: 100 },
  ]

  const edgeColumns = [
    { field: 'source', headerName: 'Source', flex: 2, minWidth: 200 },
    { field: 'target', headerName: 'Target', flex: 2, minWidth: 200 },
    { field: 'type', headerName: 'Type', flex: 1, minWidth: 100 },
  ]

  const nodeRows = useMemo(() =>
    (result?.nodes || []).map((n, i) => ({ ...n, _idx: i })),
    [result],
  )

  const edgeRows = useMemo(() =>
    (result?.edges || []).map((e, i) => ({ ...e, _idx: i })),
    [result],
  )

  return (
    <Box sx={{ p: 2, pb: 0 }}>
      <Typography variant="h6" gutterBottom sx={{ mb: 0.5 }}>
        {action.title}
      </Typography>

      {/* Query controls */}
      <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 1 }}>
        <TextField
          label="Query (node ID)"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          size="small"
          sx={{ flex: 1 }}
          disabled={isRunning}
          onKeyDown={(e) => { if (e.key === 'Enter') handleExecute() }}
          placeholder="e.g. interaction.applications.dashboards"
        />
        <TextField
          label="Depth"
          type="number"
          value={depthStr}
          onChange={(e) => {
            const v = e.target.value
            setDepthStr(v)
            const n = parseInt(v, 10)
            if (!isNaN(n)) setDepth(n)
          }}
          onBlur={() => {
            if (depthStr === '' || depthStr === '-') {
              setDepthStr(String(depth))
            }
          }}
          slotProps={{ htmlInput: { min: -1 } }}
          size="small"
          sx={{ width: 80 }}
          disabled={isRunning}
        />
        <FormControlLabel
          control={<Switch checked={reverse} onChange={(e) => setReverse(e.target.checked)} disabled={isRunning} size="small" />}
          label="Reverse"
        />
        <Button
          variant="contained"
          onClick={() => handleExecute()}
          disabled={isRunning}
          startIcon={<PlayArrow />}
          size="small"
        >
          Query
        </Button>
      </Stack>

      {result && (
        <>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
            <Chip label={`${result.stats.total_nodes} nodes`} size="small" variant="outlined" />
            <Chip label={`${result.stats.total_edges} edges`} size="small" variant="outlined" />
            {result.query.node && (
              <Chip label={`query: ${result.query.node}`} size="small" color="primary" variant="outlined" />
            )}
          </Box>

          <Tabs value={tab} onChange={(_, v) => setTab(v)} sx={{ mb: 0.5, minHeight: 36, '& .MuiTab-root': { minHeight: 36, py: 0 } }}>
            <Tab label="Nodes" />
            <Tab label="Edges" />
            <Tab label="Visual" />
          </Tabs>

          {/* Nodes tab */}
          {tab === 0 && (
            <Box sx={{ height: 500 }}>
              <DataGrid
                rows={nodeRows}
                columns={nodeColumns}
                getRowId={(row) => row._idx}
                density="compact"
                disableRowSelectionOnClick
                initialState={{ pagination: { paginationModel: { pageSize: 25 } } }}
                pageSizeOptions={[25, 50, 100]}
              />
            </Box>
          )}

          {/* Edges tab */}
          {tab === 1 && (
            <Box sx={{ height: 500 }}>
              <DataGrid
                rows={edgeRows}
                columns={edgeColumns}
                getRowId={(row) => row._idx}
                density="compact"
                disableRowSelectionOnClick
                initialState={{ pagination: { paginationModel: { pageSize: 25 } } }}
                pageSizeOptions={[25, 50, 100]}
              />
            </Box>
          )}

          {/* Visual tab */}
          {tab === 2 && (
            <Box>
              {/* Large graph warning */}
              {showLargeGraph && !largeGraphDismissed && (
                <Alert severity="warning" sx={{ mb: 2 }}>
                  <Typography variant="body2" sx={{ mb: 1 }}>
                    This graph has {result.stats.total_nodes} nodes. Rendering may be slow.
                  </Typography>
                  <Stack direction="row" spacing={1}>
                    <Button
                      size="small"
                      variant="contained"
                      onClick={() => { setLargeGraphDismissed(true); setShowLargeGraph(false) }}
                    >
                      Show all ({result.stats.total_nodes} nodes)
                    </Button>
                    <Button
                      size="small"
                      variant="outlined"
                      onClick={() => {
                        setHiddenKinds(new Set(['variable']))
                        setLargeGraphDismissed(true)
                        setShowLargeGraph(false)
                      }}
                    >
                      Hide variables
                    </Button>
                    <Button
                      size="small"
                      variant="outlined"
                      onClick={() => setTab(0)}
                    >
                      View as table
                    </Button>
                  </Stack>
                </Alert>
              )}

              {/* Legend + filters */}
              {(!showLargeGraph || largeGraphDismissed) && (
                <>
                  {/* Kind filter row */}
                  {nodeKinds.length > 0 && (
                    <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" sx={{ mb: 0.5 }}>
                      <Typography variant="caption" sx={{ color: 'text.secondary', mr: 0.5, fontWeight: 500 }}>Kind</Typography>
                      {nodeKinds.map((k) => {
                        const hidden = hiddenKinds.has(k)
                        const color = getKindColor(k)
                        return (
                          <Chip
                            key={`kind-${k}`}
                            label={k}
                            size="small"
                            onClick={() => toggleKind(k)}
                            sx={{
                              bgcolor: hidden ? 'transparent' : color,
                              color: hidden ? color : '#fff',
                              border: hidden ? `1.5px dashed ${color}` : 'none',
                              fontSize: '0.7rem', height: 20,
                              opacity: hidden ? 0.5 : 1,
                              cursor: 'pointer',
                              '&:hover': { opacity: hidden ? 0.8 : 0.9 },
                            }}
                          />
                        )
                      })}
                    </Stack>
                  )}

                  {/* Type filter row */}
                  {nodeTypes.length > 1 && (
                    <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" sx={{ mb: 0.5 }}>
                      <Typography variant="caption" sx={{ color: 'text.secondary', mr: 0.5, fontWeight: 500 }}>Type</Typography>
                      {nodeTypes.map((t) => {
                        const hidden = hiddenNodeTypes.has(t)
                        const color = getTypeColor(t)
                        return (
                          <Chip
                            key={`type-${t}`}
                            label={t}
                            size="small"
                            onClick={() => toggleNodeType(t)}
                            sx={{
                              bgcolor: hidden ? 'transparent' : color,
                              color: hidden ? color : '#fff',
                              border: hidden ? `1.5px dashed ${color}` : 'none',
                              fontSize: '0.65rem', height: 18,
                              opacity: hidden ? 0.5 : 1,
                              cursor: 'pointer',
                              '&:hover': { opacity: hidden ? 0.8 : 0.9 },
                            }}
                          />
                        )
                      })}
                    </Stack>
                  )}

                  {/* Edge filter row + controls */}
                  <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" sx={{ mb: 0.5 }}>
                    <Typography variant="caption" sx={{ color: 'text.secondary', mr: 0.5, fontWeight: 500 }}>Edge</Typography>
                    {edgeTypes.map((t) => {
                      const hidden = hiddenEdgeTypes.has(t)
                      const color = getEdgeColor(t)
                      return (
                        <Chip
                          key={`edge-${t}`}
                          label={t}
                          size="small"
                          variant="outlined"
                          onClick={() => toggleEdgeType(t)}
                          sx={{
                            borderColor: color,
                            color: color,
                            borderStyle: hidden ? 'dashed' : 'solid',
                            fontSize: '0.7rem', height: 20,
                            opacity: hidden ? 0.3 : 1,
                            textDecoration: hidden ? 'line-through' : 'none',
                            cursor: 'pointer',
                            '&:hover': { opacity: hidden ? 0.6 : 0.8 },
                          }}
                        />
                      )
                    })}
                    <Box sx={{ flexGrow: 1 }} />
                    <FormControlLabel
                      control={
                        <Switch
                          checked={hideIsolated}
                          onChange={(e) => setHideIsolated(e.target.checked)}
                          size="small"
                        />
                      }
                      label={<Typography variant="caption">Hide isolated</Typography>}
                      sx={{ mr: 0.5 }}
                    />
                    <Tooltip title="Re-layout">
                      <IconButton size="small" onClick={handleRelayout}><Refresh fontSize="small" /></IconButton>
                    </Tooltip>
                    <Tooltip title="Zoom in">
                      <IconButton size="small" onClick={handleZoomIn}><ZoomIn fontSize="small" /></IconButton>
                    </Tooltip>
                    <Tooltip title="Zoom out">
                      <IconButton size="small" onClick={handleZoomOut}><ZoomOut fontSize="small" /></IconButton>
                    </Tooltip>
                    <Tooltip title="Fit to screen">
                      <IconButton size="small" onClick={handleFit}><CenterFocusStrong fontSize="small" /></IconButton>
                    </Tooltip>
                  </Stack>

                  {/* Cytoscape container */}
                  <Box sx={{ position: 'relative' }}>
                    <Box
                      ref={cyRef}
                      sx={{
                        height: 'calc(100vh - 300px)',
                        minHeight: 500,
                        mx: -2,
                        border: '1px solid',
                        borderColor: 'divider',
                        bgcolor: 'background.default',
                      }}
                    />
                    {/* Hover tooltip */}
                    {hoverInfo && (
                      <Paper
                        elevation={4}
                        sx={{
                          position: 'absolute',
                          left: hoverInfo.x + 20,
                          top: hoverInfo.y - 10,
                          px: 1.5,
                          py: 0.75,
                          pointerEvents: 'none',
                          zIndex: 20,
                          maxWidth: 350,
                          fontSize: '0.75rem',
                          lineHeight: 1.4,
                        }}
                      >
                        <Typography variant="caption" component="div" sx={{ fontWeight: 600, wordBreak: 'break-all' }}>
                          {hoverInfo.id}
                        </Typography>
                        <Typography variant="caption" component="div" sx={{ color: 'text.secondary' }}>
                          Kind: {hoverInfo.kind} &middot; Layer: {hoverInfo.layer}
                        </Typography>
                      </Paper>
                    )}
                  </Box>
                </>
              )}
            </Box>
          )}
        </>
      )}

      {!result && runInfo && <DefaultOutput />}
    </Box>
  )
}
