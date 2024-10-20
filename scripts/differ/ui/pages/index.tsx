import React, { useState } from "react";
import { parseDiff, Diff, Hunk, tokenize, markEdits } from "react-diff-view";
import "react-diff-view/style/index.css";
import refractor from "refractor";

const Home: React.FC = () => {
  const [diffString, setDiffString] = useState<string>("");

  const [approvedChanges, setApprovedChanges] = useState<string[]>([]);

  const onDiffStringChange = (
    event: React.ChangeEvent<HTMLTextAreaElement>
  ) => {
    setDiffString(event.target.value);
  };

  if (!diffString) {
    return (
      <div className="h-full bg-gray-200">
        <div className="px-4 py-8 max-w-2xl mx-auto text-center">
          <h3 className="inline-flex items-center font-bold text-2xl uppercase mb-4">
            <span className="mr-3 text-3xl">👀</span>
            Differ
            <span className="ml-3 text-3xl">👀</span>
          </h3>
          <textarea
            className="border border-gray-300 rounded-md p-2 mt-2"
            onChange={onDiffStringChange}
            placeholder="Paste your diff..."
            style={{ width: "100%", height: "200px" }}
          />
        </div>
      </div>
    );
  }

  const files = parseDiff(diffString, { nearbySequences: "zip" });

  const renderFile = ({
    oldPath,
    newPath,
    oldRevision,
    newRevision,
    type,
    hunks,
  }) => {
    const changeId = oldRevision;
    const isApproved = approvedChanges.includes(changeId);

    const options = {
      highlight: true,
      refractor: refractor,
      language: "go",
      enhancers: [markEdits(hunks)],
    };

    const tokens = tokenize(hunks, options);

    return (
      <div className="mb-8 rounded-lg border-2 border-gray-300">
        <div className="sticky top-0">
          <div className="flex items-center py-2 text-base text-left rounded-tl-lg rounded-tr-lg border-b bg-gray-100 border-gray-300">
            <div className="flex items-center flex-grow px-4">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.5}
                stroke="currentColor"
                className="w-5 h-5 mr-1.5"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M5.25 8.25h15m-16.5 7.5h15m-1.8-13.5l-3.9 19.5m-2.1-19.5l-3.9 19.5"
                />
              </svg>
              <span>{oldPath.substring(oldPath.indexOf("@") + 1)}</span>
            </div>
            <button
              className={`px-4 py-0.5 mr-2 border rounded-md transition-colors duration-150 ${
                isApproved
                  ? "bg-blue-100 text-blue-600 hover:bg-blue-100 border-blue-300"
                  : "border-gray-400 text-gray-600 hover:bg-gray-100"
              }`}
              onClick={() => {
                if (isApproved) {
                  setApprovedChanges(
                    approvedChanges.filter((change) => change !== changeId)
                  );
                } else {
                  setApprovedChanges([...approvedChanges, changeId]);
                }
              }}
            >
              {isApproved ? "Approved" : "Approve"}
            </button>
          </div>
          <div
            className={
              "flex h-full bg-gray-100 text-sm text-gray-600 " +
              (isApproved
                ? "rounded-bl-lg rounded-br-lg"
                : "border-b border-gray-300")
            }
          >
            <div className="flex-1 py-2 px-4">
              {oldPath.substring(0, oldPath.indexOf("@"))}
            </div>
            <div className="flex-1 py-2 px-4 border-l-2 border-gray-200">
              {newPath.substring(0, newPath.indexOf("@"))}
            </div>
          </div>
        </div>
        {!isApproved && (
          <div className="bg-white rounded-bl-lg rounded-br-lg">
            <Diff
              key={oldRevision + "-" + newRevision}
              viewType="split"
              diffType={type}
              hunks={hunks}
              tokens={tokens}
            >
              {(hunks) =>
                hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)
              }
            </Diff>
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="flex flex-col h-[100vh]">
      <div className="bg-gray-100 flex py-4 px-4 items-center justify-center text-base text-left border-b-2 border-gray-300 z-10">
        <div className="pl-6 flex-grow whitespace-nowrap flex items-center">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            fill="none"
            viewBox="0 0 24 24"
            strokeWidth={1.5}
            stroke="currentColor"
            className="w-6 h-6 mr-4"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              d="M17.25 6.75L22.5 12l-5.25 5.25m-10.5 0L1.5 12l5.25-5.25m7.5-3l-4.5 16.5"
            />
          </svg>
          <span>
            Approved {approvedChanges.length}/{files.length} changes
          </span>
        </div>
        <div className="flex items-center text-sm bg-gray-200 px-3 py-1.5 rounded-md w-full max-w-2xl">
          <span className="whitespace-nowrap mr-4">Changes IDs</span>
          <input
            type="text"
            readOnly={true}
            value={JSON.stringify(approvedChanges)}
            className="w-full px-4 bg-gray-100 border border-gray-300 rounded-md p-2 text-gray-500"
            onClick={(event: React.MouseEvent<HTMLInputElement>) => {
              (event.currentTarget as HTMLInputElement).select();
            }}
          />
        </div>
      </div>

      <div className="flex-grow overflow-y-auto">
        <div className="px-4 py-8">{files.map(renderFile)}</div>
      </div>
    </div>
  );
};

export default Home;
