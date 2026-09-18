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

function DeltaSelectFrame() {
  const toolBaseUrl = useBaseUrl("/deepswe-planner/app/");
  const { colorMode } = useColorMode();
  const toolUrl = useRef(`${toolBaseUrl}?theme=${colorMode}`).current;
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
      title="DeltaSelect benchmark task selection"
      src={toolUrl}
      style={frameStyle}
      allow="clipboard-write"
      onLoad={synchronizeTheme}
    />
  );
}

/**
 * Render the self-contained DeltaSelect benchmark task-selection tool inside
 * the website shell while keeping its validated evidence layout isolated from
 * documentation theme styles.
 *
 * @returns {React.JSX.Element} DeltaSelect tool page
 */
export default function DeltaSelectToolPage() {
  return (
    <Layout
      title="DeltaSelect Tool"
      description="Choose informative coding-agent benchmark tasks within a fixed evaluation budget using DeltaSelect."
      noFooter
    >
      <DeltaSelectFrame />
    </Layout>
  );
}
