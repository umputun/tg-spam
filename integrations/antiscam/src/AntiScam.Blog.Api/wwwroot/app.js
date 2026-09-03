const postsElement = document.querySelector("#posts");
const form = document.querySelector("#post-form");
const statusElement = document.querySelector("#form-status");
const workspaceElement = document.querySelector("#workspace");
const postsHeadingElement = document.querySelector("#posts-heading");
const showAllPosts = new URLSearchParams(window.location.search).get("view") === "all";

async function loadWorkspace() {
  const response = await fetch("/api/workspace");
  const workspace = await response.json();
  workspaceElement.textContent = workspace.exists ? `Połączono z ${workspace.rootPath}` : `Brak folderu ${workspace.rootPath}`;
}

async function loadPosts() {
  const response = await fetch(showAllPosts ? "/api/posts" : "/api/posts/latest");
  if (!response.ok) { postsElement.textContent = "Nie udało się pobrać wpisów."; return; }
  const result = await response.json();
  const posts = showAllPosts ? result : [result];
  postsHeadingElement.textContent = showAllPosts ? "Wszystkie wpisy" : "Najnowszy wpis";
  postsElement.innerHTML = "";
  for (const post of posts) {
    const article = document.createElement("article");
    article.className = "post";
    article.innerHTML = `
      <h3>${escapeHtml(post.title)}</h3>
      <p class="meta">${escapeHtml(post.author)} · ${new Date(post.publishedAt).toLocaleString("pl-PL")}</p>
      <p><strong>${escapeHtml(post.summary)}</strong></p>
      <p class="content">${escapeHtml(post.content)}</p>
      <section class="comments" aria-label="Komentarze">
        <h4>Komentarze</h4><div class="comment-list" data-comment-list></div>
        <form class="comment-form" data-post-id="${post.id}">
          <label>Treść komentarza<textarea name="content" required maxlength="2000"></textarea></label>
          <label>Autor<input name="author" required maxlength="100" value="Czytelnik"></label>
          <button type="submit">Dodaj komentarz</button><p class="comment-status" role="status"></p>
        </form>
      </section>`;
    postsElement.appendChild(article);
    await loadComments(post.id, article);
  }
}

async function loadComments(postId, article) {
  const response = await fetch(`/api/posts/${postId}/comments`);
  const list = article.querySelector("[data-comment-list]");
  if (!response.ok) { list.textContent = "Nie udało się pobrać komentarzy."; return; }
  const comments = await response.json();
  list.innerHTML = comments.length === 0 ? "Brak komentarzy. Dodaj pierwszy." : comments.map(comment => `<article class="comment"><p class="meta">${escapeHtml(comment.author)} · ${new Date(comment.publishedAt).toLocaleString("pl-PL")}</p><p>${escapeHtml(comment.content)}</p></article>`).join("");
}

form.addEventListener("submit", async event => {
  event.preventDefault();
  statusElement.textContent = "Publikowanie...";
  const response = await fetch("/api/posts", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(Object.fromEntries(new FormData(form).entries())) });
  if (!response.ok) {
    if (response.status === 422) { const problem = await response.json(); statusElement.textContent = problem.aiExplanation ?? problem.risk.blockExplanation; return; }
    statusElement.textContent = "Nie udało się opublikować wpisu.";
    return;
  }
  form.reset();
  statusElement.textContent = "Wpis opublikowany.";
  await loadPosts();
});

postsElement.addEventListener("submit", async event => {
  const commentForm = event.target.closest(".comment-form");
  if (!commentForm) return;
  event.preventDefault();
  const status = commentForm.querySelector(".comment-status");
  status.textContent = "Publikowanie komentarza...";
  const response = await fetch(`/api/posts/${commentForm.dataset.postId}/comments`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(Object.fromEntries(new FormData(commentForm).entries())) });
  if (!response.ok) {
    if (response.status === 422) { const problem = await response.json(); status.textContent = problem.risk?.blockExplanation ?? "Komentarz został zablokowany przez ochronę antyscamową."; return; }
    status.textContent = "Nie udało się opublikować komentarza.";
    return;
  }
  commentForm.reset();
  status.textContent = "Komentarz opublikowany.";
  await loadComments(Number(commentForm.dataset.postId), commentForm.closest(".post"));
});

function escapeHtml(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#039;");
}

loadWorkspace();
loadPosts();
