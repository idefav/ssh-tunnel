// SSH-Tunnel GitHub Pages 主JavaScript文件
document.addEventListener('DOMContentLoaded', function() {
    // 激活当前页面导航链接
    const currentPage = window.location.pathname.split('/').pop();
    const navLinks = document.querySelectorAll('.navbar-nav .nav-link');

    navLinks.forEach(link => {
        const href = link.getAttribute('href');
        if (href === currentPage ||
            (currentPage === '' && href === 'index.html')) {
            link.classList.add('active');
        } else {
            link.classList.remove('active');
        }
    });

    // 平滑滚动到锚点
    document.querySelectorAll('a[href^="#"]').forEach(anchor => {
        anchor.addEventListener('click', function (e) {
            e.preventDefault();

            const targetId = this.getAttribute('href');
            if (targetId === '#') return;

            const targetElement = document.querySelector(targetId);
            if (targetElement) {
                targetElement.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start'
                });
            }
        });
    });

    // 代码块复制功能
    document.querySelectorAll('pre code').forEach(block => {
        const copyBtn = document.createElement('button');
        copyBtn.className = 'copy-btn';
        copyBtn.innerHTML = '<i class="bi bi-clipboard"></i>';
        copyBtn.title = '复制代码';

        copyBtn.addEventListener('click', () => {
            const textToCopy = block.textContent;
            navigator.clipboard.writeText(textToCopy).then(() => {
                copyBtn.innerHTML = '<i class="bi bi-check"></i>';
                setTimeout(() => {
                    copyBtn.innerHTML = '<i class="bi bi-clipboard"></i>';
                }, 2000);
            });
        });

        const wrapper = document.createElement('div');
        wrapper.className = 'code-wrapper position-relative';

        block.parentNode.insertBefore(wrapper, block);
        wrapper.appendChild(block);
        wrapper.appendChild(copyBtn);
    });
});
