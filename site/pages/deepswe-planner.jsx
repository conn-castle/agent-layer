import React, { useCallback, useEffect, useRef } from "react";
import Layout from "@theme/Layout";
import useBaseUrl from "@docusaurus/useBaseUrl";
import { useColorMode } from "@docusaurus/theme-common";

const frameStyle = {
  display: "block",
  width: "100%",
  minHeight: "calc(100vh - 60px)",
  border: 0,
  background: "transparent",
};

function PlannerFrame() {
  const plannerBaseUrl = useBaseUrl("/deepswe-planner/app/");
  const { colorMode } = useColorMode();
  const plannerUrl = useRef(`${plannerBaseUrl}?theme=${colorMode}`).current;
  const frameRef = useRef(null);

  const synchronizeTheme = useCallback(() => {
    if (colorMode !== "light" && colorMode !== "dark") return;
    frameRef.current?.contentWindow?.postMessage(
      { type: "agent-layer-theme", theme: colorMode },
      window.location.origin,
    );
  }, [colorMode]);

  useEffect(synchronizeTheme, [synchronizeTheme]);

  return (
    <iframe
      ref={frameRef}
      title="DeepSWE task correlation and cost"
      src={plannerUrl}
      style={frameStyle}
      allow="clipboard-write"
      onLoad={synchronizeTheme}
    />
  );
}

/**
 * Render the self-contained DeepSWE task evidence table inside the website
 * shell. The iframe keeps the validated evidence layout isolated from
 * documentation theme styles while preserving same-origin clipboard access.
 *
 * @returns {React.JSX.Element} website task-evidence page
 */
export default function DeepSWEPlannerPage() {
  return (
    <Layout
      title="DeepSWE task evidence"
      description="Compare DeepSWE task correlation, calibrated composite weight, and estimated published score and price."
      noFooter
    >
      <PlannerFrame />
    </Layout>
  );
}
