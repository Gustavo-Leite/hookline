import http from "k6/http";
import { check } from "k6";

const baseURL = __ENV.BASE_URL || "http://localhost:8080";
const apiKey = __ENV.HOOKLINE_KEY;

export const options = {
  stages: [
    { duration: "10s", target: 25 },
    { duration: "40s", target: 25 },
    { duration: "5s", target: 0 },
  ],
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<200"],
  },
};

export default function () {
  const response = http.post(
    `${baseURL}/v1/events`,
    JSON.stringify({
      type: "user.created",
      payload: { id: `${__VU}-${__ITER}` },
    }),
    {
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiKey}`,
      },
    },
  );

  check(response, {
    "accepted": (r) => r.status === 202,
  });
}
