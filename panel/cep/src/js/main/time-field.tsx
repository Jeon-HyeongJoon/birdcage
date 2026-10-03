// 시간 입력: 입력한 글자를 유지하다가 해석되면(blur/Enter) 반영한다. "1:23.5", "1m30s", "90".
import { useEffect, useState } from "react";
import { formatTime, parseTime } from "@birdcage/core";

/** Time input that keeps the typed text until it parses (blur/Enter commits). */
export const TimeField = ({
  label,
  value,
  placeholder = "0:00.0",
  onCommit,
}: {
  label: string;
  value?: number;
  placeholder?: string;
  onCommit: (v?: number) => void;
}) => {
  const [text, setText] = useState(value === undefined ? "" : formatTime(value));
  const [bad, setBad] = useState(false);
  useEffect(() => {
    setText(value === undefined ? "" : formatTime(value));
    setBad(false);
  }, [value]);
  const commit = () => {
    if (text.trim() === "") {
      onCommit(undefined);
      return;
    }
    const v = parseTime(text);
    setBad(v === undefined);
    if (v !== undefined) onCommit(v);
  };
  return (
    <label className="time">
      <span className="muted">{label}</span>
      <input
        type="text"
        className={bad ? "bad" : ""}
        value={text}
        placeholder={placeholder}
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            commit();
          }
        }}
      />
    </label>
  );
};
