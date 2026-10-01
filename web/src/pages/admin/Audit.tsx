import { PageHeader } from '../../components/PageHeader'
import { AuditView } from '../../components/AuditView'

export default function Audit() {
  return (
    <>
      <PageHeader title="감사 로그" description="모든 MCP 도구 호출, 로그인, OAuth, 키·매핑 변경, 관리 작업이 기록됩니다. 메시지 본문은 기본적으로 저장하지 않고 질의 해시와 건수만 남깁니다." />
      <AuditView endpoint="/admin/audit" admin />
    </>
  )
}
