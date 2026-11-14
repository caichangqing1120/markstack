const repository = { owner: 'owner', repo: 'markstack' };

function fixture(overrides = {}) {
  const state = {
    issue: { number: 7, state: 'open', assignees: [], ...overrides.issue },
    comments: [],
    pages: overrides.pages || [[]],
    pulls: new Map(),
  };
  const pull = {
    number: 11,
    user: { login: 'alice', type: 'User' },
    body: 'Closes #7\n\nImplement the agreed issue scope with regression coverage.',
    merged_at: null,
    base: { ref: 'main', repo: { full_name: 'owner/markstack' } },
    head: { sha: 'abc123' },
    ...overrides.pull,
  };
  state.pulls.set(11, pull);
  const github = {
    rest: {
      repos: {
        get: async () => ({ data: { default_branch: 'main' } }),
      },
      issues: {
        get: async () => ({ data: structuredClone(state.issue) }),
        addAssignees: async ({ assignees }) => {
          if (overrides.assignmentError) throw overrides.assignmentError;
          if (!overrides.ignoreAssignment) state.issue.assignees = assignees.map(login => ({ login }));
          return { data: structuredClone(state.issue) };
        },
        removeAssignees: async ({ assignees }) => {
          state.issue.assignees = state.issue.assignees.filter(user => !assignees.includes(user.login));
          return { data: structuredClone(state.issue) };
        },
        createComment: async ({ body }) => { state.comments.push(body); },
        update: async ({ state: nextState, state_reason }) => {
          state.issue.state = nextState;
          state.issue.state_reason = state_reason;
        },
        listEventsForTimeline: async () => ({ data: state.pages[0] }),
      },
      pulls: {
        get: async ({ pull_number }) => ({ data: structuredClone(state.pulls.get(pull_number)) }),
        listFiles: async () => ({ data: overrides.changedFiles || [] }),
      },
    },
    paginate: async (route, options) => route === github.rest.issues.listEventsForTimeline
      ? state.pages.flat()
      : (await route(options)).data,
  };
  const context = {
    repo: repository,
    actor: 'alice',
    payload: {
      issue: { number: 7 },
      comment: { body: '/claim', user: { login: 'alice', type: 'User' } },
      pull_request: pull,
      repository: { default_branch: 'main' },
    },
  };
  return { state, github, context };
}

function linkedPull(number = 11, repo = 'owner/markstack') {
  return {
    event: 'cross-referenced',
    source: { issue: { pull_request: { url: `https://api.github.com/repos/${repo}/pulls/${number}` } } },
  };
}

module.exports = { fixture, linkedPull };
