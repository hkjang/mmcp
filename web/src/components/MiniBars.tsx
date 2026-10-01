import { Tooltip, theme } from 'antd'
import dayjs from 'dayjs'

export interface HourPoint { hour: string; success: number; failure: number; denied: number }

/** 24h stacked bar chart (inline SVG, no chart library needed offline). */
export function HourlyBars({ data, height = 140 }: { data: HourPoint[]; height?: number }) {
  const { token } = theme.useToken()
  const now = dayjs().startOf('hour')
  const hours = Array.from({ length: 24 }, (_, i) => now.subtract(23 - i, 'hour'))
  const byHour = new Map(data.map((d) => [dayjs(d.hour).startOf('hour').valueOf(), d]))
  const series = hours.map((h) => byHour.get(h.valueOf()) ?? { hour: h.toISOString(), success: 0, failure: 0, denied: 0 })
  const max = Math.max(1, ...series.map((d) => d.success + d.failure + d.denied))
  const w = 100 / 24
  return (
    <div>
      <svg viewBox={`0 0 100 ${height}`} preserveAspectRatio="none" style={{ width: '100%', height }} role="img" aria-label="최근 24시간 도구 호출">
        {[0.25, 0.5, 0.75].map((f) => (
          <line key={f} x1="0" x2="100" y1={height * f} y2={height * f} stroke={token.colorBorderSecondary} strokeWidth="0.3" vectorEffect="non-scaling-stroke" />
        ))}
        {series.map((d, i) => {
          const total = d.success + d.failure + d.denied
          const hS = (d.success / max) * (height - 4)
          const hF = (d.failure / max) * (height - 4)
          const hD = (d.denied / max) * (height - 4)
          const x = i * w + w * 0.18
          const bw = w * 0.64
          return (
            <Tooltip key={i} title={`${dayjs(d.hour).format('MM-DD HH:00')} · 성공 ${d.success} / 실패 ${d.failure} / 거부·대기 ${d.denied}`}>
              <g className="mmcp-chart-bar">
                <rect x={i * w} y={0} width={w} height={height} fill="transparent" />
                <rect x={x} y={height - hS} width={bw} height={hS} rx="0.6" fill={token.colorPrimary} />
                <rect x={x} y={height - hS - hF} width={bw} height={hF} fill={token.colorError} />
                <rect x={x} y={height - hS - hF - hD} width={bw} height={hD} fill={token.colorWarning} />
                {total === 0 && <rect x={x} y={height - 1.2} width={bw} height={1.2} fill={token.colorBorder} />}
              </g>
            </Tooltip>
          )
        })}
      </svg>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: token.colorTextTertiary, marginTop: 4 }}>
        <span>{hours[0].format('HH:00')}</span>
        <span>{hours[12].format('HH:00')}</span>
        <span>지금</span>
      </div>
    </div>
  )
}
