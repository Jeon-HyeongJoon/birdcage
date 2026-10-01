import React from "react";
import ReactDOM from "react-dom/client";
import { initBolt } from "../lib/utils/bolt";
import "../index.scss";
import { App } from "./main";

initBolt();

// StrictMode는 개발 중 effect를 두 번 실행한다. Controller.start()는 중복 호출에 안전하다.
ReactDOM.createRoot(document.getElementById("app") as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
