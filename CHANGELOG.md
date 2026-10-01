# Changelog

## v1.0.0 — 2026-10-01

첫 정식 릴리스.

- **MCP 게이트웨이**: Streamable HTTP(2025-11-25/06-18/03-26), 도구 26종(채널·검색·스레드·DM·그룹 DM·파일·채널 관리), 리소스·프롬프트, 공식 Mattermost MCP 호환 별칭.
- **인증**: Keycloak OIDC 로그인 + 사일런트 SSO(prompt=none) + JIT 사용자 생성·역할 동기화, MCP OAuth 2.1 인가 서버(DCR, PKCE S256, 회전형 리프레시 토큰·재사용 감지), Keycloak 액세스 토큰 직접 검증(JWKS), 개인 API 키.
- **아이덴티티 매핑**: preferred_username으로 최초 탐색 후 Keycloak sub ↔ Mattermost user_id 고정, 계정 삭제·재생성 충돌 자동 차단, 수동 매핑.
- **자격증명**: 사용자 PAT 자동 발급/직접 등록, 발신 계정 선택(본인/봇/자동), 봇 발신 시 요청자 표시와 props 기록.
- **보안**: 봉투 암호화(마스터 키 → 사용자별 데이터 키, AES-256-GCM, AAD 바인딩), 개인 키 회전, 마스터 키 무중단 회전(재래핑), 쓰기 도구 2단계 승인, 채널 유형 정책, 사용자별 호출 한도, IP 허용목록, 신뢰불가 콘텐츠 래핑, 감사 로그.
- **콘솔**: 관리 콘솔과 개인 공간 분리, 명령 팔레트(초성 검색)·G 단축키, 실시간 서버 로그, 도구 실행기, 백업·복원, 다크 모드, 모바일 대응.
- **배포**: 환경변수 4개(POSTGRES_DSN, BOOTSTRAP_ADMIN, BOOTSTRAP_ADMIN_PASSWORD, ENCRYPTION_KEY)만으로 동작하는 distroless 단일 이미지, 오프라인 tar.gz 릴리스.
