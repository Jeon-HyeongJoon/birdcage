// 상단 상태: 초록(사용 가능)·빨강(문제) 점 하나. 자세한 내용은 점에 마우스를 올리면 보인다(사용자 지시 2026-10-03).
import type { State } from "@birdcage/core";
import type { PanelController } from "./main";

const HEIGHTS = [360, 480, 720, 1080, 1440, 2160];

export const StatusDot = ({ s, missing, retry }: { s: State; missing: string[]; retry: () => void }) => {
  const ok = s.connection === "connected" && missing.length === 0;
  let why = "사용 가능";
  if (s.connection === "starting" || s.connection === "idle") why = "준비 중(Helper 시작)";
  else if (s.connection === "reconnecting") why = "다시 연결하는 중";
  else if (s.connection === "failed") why = `연결 실패: ${s.error ?? ""} — 눌러서 다시 시도`;
  else if (missing.length) why = `필요한 도구 없음: ${missing.join(", ")}`;
  const detail = s.helper ? `\nHelper ${s.helper.helperVersion}` : "";
  return (
    <span
      className={`dot ${ok ? "ok" : "bad"}`}
      role="status"
      aria-label={ok ? "사용 가능" : "문제 있음"}
      title={why + detail}
      onClick={s.connection === "failed" ? retry : undefined}
    />
  );
};

/** 받기 설정 한 줄: 완료되면 바로 넣기, 기본 화질. 넣는 위치는 항상 재생헤드의 새 빈 트랙이다. */
export const SettingsRow = ({ s, ctl }: { s: State; ctl: Pick<PanelController, "updateSettings"> }) => (
  <div className="row options muted small">
    <label title="받기가 끝나면 열린 시퀀스의 재생헤드 위치, 새 빈 트랙에 넣습니다(기존 클립은 그대로). 시퀀스가 없으면 소스 모니터로 엽니다.">
      <input type="checkbox" checked={s.settings.autoImport} onChange={(e) => ctl.updateSettings({ autoImport: e.target.checked })} />
      완료되면 새 트랙에 바로 넣기
    </label>
    <label>
      기본 화질
      <select value={s.settings.maxHeight} onChange={(e) => ctl.updateSettings({ maxHeight: Number(e.target.value) })}>
        {HEIGHTS.map((h) => (
          <option key={h} value={h}>
            {h}p
          </option>
        ))}
      </select>
    </label>
  </div>
);
