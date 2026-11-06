import { afterEach, describe, expect, it } from 'vitest'
import { init, use, type EChartsType } from 'echarts/core'
import { GraphChart } from 'echarts/charts'
import { SVGRenderer } from 'echarts/renderers'
import { MEMORY_GRAPH_SERIES_ID } from './constants'
import { memoryGraphSeries, nodeDataIndex, readSeriesCenter, readSeriesZoom, tooltipPositionForNode } from './build-chart-option'

use([GraphChart, SVGRenderer])
const charts: EChartsType[] = []
function graph() {
  const chart = init(null, undefined, { renderer: 'svg', ssr: true, width: 600, height: 400 })
  charts.push(chart)
  chart.setOption({
    animation: false,
    series: [{
      id: MEMORY_GRAPH_SERIES_ID, type: 'graph', layout: 'none', zoom: 1.5, center: [10, 20],
      data: [{ id: 'first', x: 0, y: 0 }, { id: 'second', x: 100, y: 100 }], links: [],
    }],
  })
  return chart
}
afterEach(() => charts.splice(0).forEach(chart => chart.dispose()))

describe('installed ECharts live graph contract', () => {
  it('keeps live zoom, center, node identity and tooltip coordinates on a real chart', () => {
    const chart = graph()
    expect(memoryGraphSeries(chart)?.subType).toBe('graph')
    expect(readSeriesZoom(chart)).toBe(1.5)
    expect(readSeriesCenter(chart)).toEqual([10, 20])
    expect(nodeDataIndex(chart, 'second')).toBe(1)
    expect(nodeDataIndex(chart, 'absent')).toBeNull()
    const position = tooltipPositionForNode(chart, { dataType: 'node', dataIndex: 1, seriesIndex: 0 }, { contentSize: [50, 20], viewSize: [600, 400] })
    expect(position).not.toBeNull()
    expect(position?.every(Number.isFinite)).toBe(true)
  })

  it.each([
    undefined,
    () => null,
    () => ({ getSeries: () => ({}) }),
    () => ({ getSeries: () => [{ id: MEMORY_GRAPH_SERIES_ID, subType: 'graph' }] }),
  ])('rejects a missing or incompatible internal model accessor without inventing layout data', (accessor) => {
    const chart = graph()
    Object.defineProperty(chart, 'getModel', { value: accessor, configurable: true })
    expect(memoryGraphSeries(chart)).toBeUndefined()
    expect(nodeDataIndex(chart, 'second')).toBeNull()
    expect(readSeriesCenter(chart)).toBeNull()
    expect(readSeriesZoom(chart)).toBe(1)
  })
})
