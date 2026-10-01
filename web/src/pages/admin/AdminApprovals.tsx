import { useState } from 'react'
import { Segmented, Space } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { get } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { ApprovalList, type Approval } from '../../components/ApprovalList'

export default function AdminApprovals() {
  const [status, setStatus] = useState('')
  const { data, isLoading } = useQuery({ queryKey: ['admin', 'approvals', status], queryFn: () => get<{ items: Approval[] }>('/admin/approvals', { status }) })
  return (
    <>
      <PageHeader title="승인 이력" description="쓰기 도구의 2단계 승인 요청 기록입니다. AI는 미리보기를 사용자에게 보여 주고, 사용자가 동의해야 confirm_token으로 실행할 수 있습니다." />
      <Space style={{ marginBottom: 12 }}>
        <Segmented value={status} onChange={(v) => setStatus(String(v))} options={[{ label: '전체', value: '' }, { label: '대기', value: 'pending' }, { label: '실행됨', value: 'executed' }, { label: '거절', value: 'rejected' }, { label: '만료', value: 'expired' }]} />
      </Space>
      <ApprovalList items={data?.items} loading={isLoading} showUser />
    </>
  )
}
