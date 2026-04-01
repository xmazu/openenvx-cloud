export default {
  async fetch(request, env, ctx) {
    return new Response("Hello OpenEnvX Cloudflare Worker!", { status: 200 });
  },
};
