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

    // 打字机效果实现
    const typingElement = document.getElementById('typing-text');
    if (typingElement) {
        const slogan = "仅需一个SSH连接 · 零服务端安装 · 即开即用";
        let charIndex = 0;
        const typingSpeed = 100; // 打字速度（毫秒）

        function typeText() {
            if (charIndex < slogan.length) {
                typingElement.textContent += slogan.charAt(charIndex);
                charIndex++;
                setTimeout(typeText, typingSpeed);
            }
        }

        // 开始打字效果
        setTimeout(typeText, 500);
    }

    // 平滑吸附导航栏效果
    const navbar = document.querySelector('.navbar');
    const headerElement = document.querySelector('header');
    const navSloganElement = document.getElementById('nav-slogan');
    const navbarBrand = document.querySelector('.navbar-brand');

    if (navbar && headerElement && navSloganElement) {
        // 导航栏初始位置
        const navbarOriginalTop = navbar.offsetTop;
        const headerBottom = headerElement.offsetTop + headerElement.offsetHeight;
        let isNavbarFixed = false;
        let isNavSloganVisible = false;
        let lastScrollTop = 0;

        // 实现平滑吸附效果
        function handleScroll() {
            const currentScrollTop = window.scrollY;
            const scrollingDown = currentScrollTop > lastScrollTop;

            // 判断是否应该固定导航栏
            if (currentScrollTop > navbarOriginalTop && !isNavbarFixed) {
                // 往下滚动且超过导航栏原始位置时，先固定导航栏再添加动画类
                document.body.style.paddingTop = navbar.offsetHeight + 'px';
                navbar.style.position = 'sticky';
                navbar.style.top = '0'; // 直接固定在顶部，不预先隐藏
                navbar.style.left = '0';
                navbar.style.right = '0';
                navbar.style.zIndex = '1030';
                navbar.style.transform = 'translateY(0)'; // 无位移

                // 添加视觉变化类，而不是位置变化
                navbar.classList.add('navbar-scrolled');
                isNavbarFixed = true;
            } else if (currentScrollTop <= navbarOriginalTop && isNavbarFixed) {
                // 往上滚动且回到导航栏原始位置，平滑过渡
                navbar.classList.remove('navbar-scrolled');

                // 直接恢复原始状态，不添加上移动画
                navbar.style.position = '';
                navbar.style.top = '';
                navbar.style.left = '';
                navbar.style.right = '';
                navbar.style.zIndex = '';
                navbar.style.transform = '';
                document.body.style.paddingTop = '0';
                isNavbarFixed = false;
            }

            // 处理标识的显示和隐藏
            if (currentScrollTop > headerBottom && !isNavSloganVisible) {
                navSloganElement.classList.remove('d-none');
                navSloganElement.classList.remove('fade-out-right');
                navSloganElement.classList.add('fade-in-right');
                isNavSloganVisible = true;

                // 标题动画效果
                navbarBrand.style.color = '#ffffff';
            } else if (currentScrollTop <= headerBottom && isNavSloganVisible) {
                navSloganElement.classList.remove('fade-in-right');
                navSloganElement.classList.add('fade-out-right');
                isNavSloganVisible = false;

                // 标题恢复
                navbarBrand.style.color = '';

                setTimeout(() => {
                    if (!isNavSloganVisible) {
                        navSloganElement.classList.add('d-none');
                    }
                }, 500);
            }

            lastScrollTop = currentScrollTop;
        }

        // 监听滚动事件
        window.addEventListener('scroll', handleScroll);

        // 页面加载时立即检查滚动位置
        handleScroll();
    }

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
