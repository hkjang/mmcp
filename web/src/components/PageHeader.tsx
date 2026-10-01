import type { ReactNode } from 'react'
import { Space } from 'antd'

export function PageHeader({ title, description, extra }: { title: ReactNode; description?: ReactNode; extra?: ReactNode }) {
  return (
    <div className="mmcp-page-header">
      <div>
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {extra && <Space wrap>{extra}</Space>}
    </div>
  )
}
