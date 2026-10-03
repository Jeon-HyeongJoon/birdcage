// 가져오기 설정: 가져온 뒤 어디에 둘지(세그먼트), 자동 가져오기, 기본 화질.
// 배치는 받기를 시작할 때 작업에 묶인다(받는 중에 바꿔도 이미 시작한 작업은 그대로).
import type { Placement, State } from "@birdcage/core";
import type { PanelController } from "./main";

const HEIGHTS = [360, 480, 720, 1080, 1440, 2160];

const PLACEMENTS: Array<{ id: Placement; label: string; hint: string }> = [
  { id: "project", label: "프로젝트", hint: "Birdcage/<플랫폼> 빈에만 넣습니다." },
  { id: "source", label: "소스 모니터", hint: "빈에 넣고 소스 모니터에서 엽니다(구간이면 In/Out 확인)." },
  { id: "track", label: "빈 트랙", hint: "재생 헤드 위치에서 비어 있는 가장 낮은 트랙에 놓습니다. 기존 클립은 건드리지 않습니다." },
  { id: "insert", label: "삽입", hint: "재생 헤드에 끼워 넣고 뒤의 클립을 밀어냅니다(타겟 트랙)." },
  { id: "overwrite", label: "덮어쓰기", hint: "재생 헤드부터 타겟 트랙의 클립을 덮어씁니다." },
];

export const ImportSettings = ({ s, ctl }: { s: State; ctl: Pick<PanelController, "updateSettings"> }) => {
  const cur = PLACEMENTS.find((p) => p.id === s.settings.placement) ?? PLACEMENTS[0];
  return (
    <div className="section import-settings">
      <div className="section-label">가져온 뒤</div>
      <div className="seg" role="radiogroup" aria-label="가져온 뒤">
        {PLACEMENTS.map((p) => (
          <button
            key={p.id}
            type="button"
            role="radio"
            aria-checked={p.id === cur.id}
            className={p.id === cur.id ? "on" : ""}
            onClick={() => ctl.updateSettings({ placement: p.id })}
          >
            {p.label}
          </button>
        ))}
      </div>
      <div className="muted small hint">{cur.hint}</div>
      <div className="row options muted small">
        <label>
          <input
            type="checkbox"
            checked={s.settings.autoImport}
            onChange={(e) => ctl.updateSettings({ autoImport: e.target.checked })}
          />
          완료되면 바로 가져오기
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
    </div>
  );
};
