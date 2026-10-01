# 0001. CEP 패널 하나로 Premiere 15.0–26 커버, UXP는 v2

- 상태: 초안 (2026-10-02)
- 요구: Windows + macOS, Premiere 2021(15.x) 이상 + 26 모두 지원

## 결정
- V0/MVP는 **CEP 패널 하나**(`RequiredRuntime CSXS 10`, `PPRO [15.0,99.9]`)로 전 버전을 커버한다.
- UXP 패널(26+)은 v2. CEP 패널 상한을 25.x로 낮춰 26에서 중복 표시를 피한다.
- 호스트 차이는 `HostAdapter`(panel/core/src/host.ts) 하나로 가둔다.

## 근거
- Premiere 26에서도 CEP가 동작한다(26.3.2에서 CEP 브리지 검증 사례). Adobe 일정: CEP 신규 마켓 등록 종료 2027-12, 기본 비활성화 2028-12, 제거 2029-12.
- CEP 버전 매핑: 15.0–15.3 = CEP 10(Chromium 74, Node 12.3.1), 15.4+ = CEP 11, 25.0 = CEP 12 → CSXS 11/12를 요구하면 15.0–15.3에서 로드 불가.
- CEP는 Node.js가 있어 Helper를 자식 프로세스로 바로 띄울 수 있다(UXP는 불가 → v2 과제).
- Premiere ≤25에서 패널 UI를 띄우는 공식 경로는 CEP(CEF=Chromium)뿐(ScriptUI 미지원, Adobe 포럼 Bruce Bullis).

## 재검토 조건
- Adobe가 CEP 일정을 앞당기거나 26의 CEP 지원에 결함이 발견될 때.
- UXP에서 프로세스 실행이 쉬워질 때(UXP 패널 + 숨김 CEP 확장 / Hybrid 애드온(26.2+) 중 택1은 v2에서 결정).
