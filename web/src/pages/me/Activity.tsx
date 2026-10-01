import { PageHeader } from '../../components/PageHeader'
import { AuditView } from '../../components/AuditView'

export default function Activity() {
  return (
    <>
      <PageHeader title="내 활동" description="내 계정으로 실행된 도구 호출, 로그인, 키 변경 기록입니다. 모르는 기록이 있다면 키를 폐기하고 관리자에게 알리세요." />
      <AuditView endpoint="/me/activity" />
    </>
  )
}
