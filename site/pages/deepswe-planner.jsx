import React, { useCallback, useEffect, useRef } from "react";
import Layout from "@theme/Layout";
import Link from "@docusaurus/Link";
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
      title="DeltaSelect DeepSWE benchmark task selection"
      src={plannerUrl}
      style={frameStyle}
      allow="clipboard-write"
      onLoad={synchronizeTheme}
    />
  );
}

/**
 * Render the self-contained DeltaSelect task evidence table inside the website
 * shell. The iframe keeps the validated evidence layout isolated from
 * documentation theme styles while preserving same-origin clipboard access.
 *
 * @returns {React.JSX.Element} website task-evidence page
 */
export default function DeepSWEPlannerPage() {
  return (
    <Layout
      title="DeepSWE Task Planner"
      description="Choose DeepSWE benchmark tasks with calibrated weights, expected score, cost, and uncertainty using the interactive Agent Layer planner."
      noFooter
    >
      <div className="container margin-vert--md">
        <p>
          This interactive planner is the DeepSWE application of <Link to="/deltaselect">DeltaSelect</Link>,
          Agent Layer's method for selecting informative benchmark tasks within a fixed evaluation budget.
        </p>
      </div>
      <PlannerFrame />
    </Layout>
  );
}
