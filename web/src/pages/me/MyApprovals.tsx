import { useState } from 'react'
import { App, Segmented, Space, Alert } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, post, errorMessage } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { ApprovalList, type Approval } from '../../components/ApprovalList'

export default function MyApprovals() {
  const [status, setStatus] = useState('pending')
  const qc = useQueryClient()
  const { message } = App.useApp()
  const { data, isLoading } = useQuery({ queryKey: ['me', 'approvals', status], queryFn: () => get<{ items: Approval[] }>('/me/approvals', { status }) })
  const decide = useMutation({
    mutationFn: ({ id, d }: { id: string; d: 'approve' | 'reject' }) => post(`/me/approvals/${id}/${d}`),
    onSuccess: (_, v) => { message.success(v.d === 'approve' ? '승인했습니다. AI가 confirm_token으로 다시 호출하면 실행됩니다.' : '거절했습니다'); void qc.invalidateQueries({ queryKey: ['me', 'approvals'] }) },
    onError: (e) => message.error(errorMessage(e)),
  })
  return (
    <>
      <PageHeader title="승인 요청" description="AI 클라이언트가 요청한 메시지·DM 발송 같은 쓰기 작업입니다. 대화창에서 동의하면 실행되고, 여기서 미리 승인하거나 거절할 수도 있습니다." />
      <Alert type="info" showIcon style={{ marginBottom: 12 }} title="의도하지 않은 요청이 보이면 '거절'하세요. 거절된 confirm_token으로는 절대 실행되지 않습니다." />
      <Space style={{ marginBottom: 12 }}>
        <Segmented value={status} onChange={(v) => setStatus(String(v))} options={[{ label: '대기', value: 'pending' }, { label: '실행됨', value: 'executed' }, { label: '거절', value: 'rejected' }, { label: '만료', value: 'expired' }, { label: '전체', value: '' }]} />
      </Space>
      <ApprovalList items={data?.items} loading={isLoading} onDecide={(id, d) => decide.mutate({ id, d })} />
    </>
  )
}
