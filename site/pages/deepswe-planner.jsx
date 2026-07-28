import React from "react";
import Layout from "@theme/Layout";
import useBaseUrl from "@docusaurus/useBaseUrl";

const frameStyle = {
  display: "block",
  width: "100%",
  minHeight: "calc(100vh - 60px)",
  border: 0,
  background: "#f3f5f2",
};

/**
 * Render the self-contained DeepSWE experiment planner inside the website
 * shell. The iframe keeps the validated planner layout isolated from
 * documentation theme styles while preserving same-origin clipboard access.
 *
 * @returns {React.JSX.Element} website planner page
 */
export default function DeepSWEPlannerPage() {
  const plannerUrl = useBaseUrl("/deepswe-planner/app/");

  return (
    <Layout
      title="DeepSWE benchmark planner"
      description="Choose DeepSWE tasks and repetitions that maximize historical detectability for a baseline budget."
      noFooter
    >
      <iframe
        title="DeepSWE benchmark planner"
        src={plannerUrl}
        style={frameStyle}
        allow="clipboard-write"
      />
    </Layout>
  );
}
