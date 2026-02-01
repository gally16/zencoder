const API_BASE = '/api';
const REFRESH_INTERVAL = 3000;
let autoRefreshTimer = null;

// Admin Password Management
let adminPassword = null;
const ADMIN_STORAGE_KEY = 'zencoder_admin_pass';

// State
let currentState = {
    page: 1,
    size: 10,
    category: 'normal',
    total: 0,
    selectedIds: new Set(),
    items: []
};

// --- Admin Password Management ---
function savePassword(password) {
    try {
        localStorage.setItem(ADMIN_STORAGE_KEY, password);
        console.log('Password saved to localStorage');
        return true;
    } catch (e) {
        console.error('Failed to save password to localStorage:', e);
        return false;
    }
}

function getSavedPassword() {
    try {
        const saved = localStorage.getItem(ADMIN_STORAGE_KEY);
        if (saved) {
            console.log('Found saved password in localStorage');
        }
        return saved;
    } catch (e) {
        console.error('Failed to get password from localStorage:', e);
        return null;
    }
}

function clearSavedPassword() {
    try {
        localStorage.removeItem(ADMIN_STORAGE_KEY);
    } catch (e) {
        console.error('Failed to clear password from localStorage:', e);
    }
}

async function verifyAdminPassword(password) {
    try {
        // 尝试调用一个需要管理密码的API来验证
        const response = await fetch(`${API_BASE}/accounts?page=1&size=1`, {
            headers: {
                'X-Admin-Password': password
            }
        });
        return response.ok;
    } catch (e) {
        return false;
    }
}

function showAdminLogin() {
    document.getElementById('adminPasswordModal').classList.remove('hidden');
    document.getElementById('mainApp').classList.add('hidden');
    document.getElementById('adminPassword').focus();
}

function hideAdminLogin() {
    document.getElementById('adminPasswordModal').classList.add('hidden');
    document.getElementById('mainApp').classList.remove('hidden');
    document.getElementById('mainApp').classList.add('flex');
}

async function handleAdminLogin(password, remember = false) {
    console.log('Attempting login, remember:', remember);
    
    const isValid = await verifyAdminPassword(password);
    
    if (isValid) {
        adminPassword = password;
        
        if (remember) {
            const saved = savePassword(password);
            if (!saved) {
                console.warn('Failed to save password to localStorage');
            }
        } else {
            // 如果没有勾选记住，清除之前保存的密码
            clearSavedPassword();
        }
        
        hideAdminLogin();
        document.getElementById('passwordError').classList.add('hidden');
        
        // 开始加载数据
        initializeApp();
        return true;
    } else {
        document.getElementById('passwordError').classList.remove('hidden');
        return false;
    }
}

function logout() {
    adminPassword = null;
    clearSavedPassword();
    
    // 停止自动刷新
    if (autoRefreshTimer) {
        clearInterval(autoRefreshTimer);
        autoRefreshTimer = null;
    }
    
    // 显示登录界面
    showAdminLogin();
}

async function initAdminAuth() {
    // 检查是否有保存的密码
    const savedPassword = getSavedPassword();
    
    if (savedPassword) {
        console.log('Found saved password, attempting auto-login...');
        // 直接设置密码，不验证（因为验证可能由于网络问题失败）
        adminPassword = savedPassword;
        
        // 尝试验证密码
        try {
            const isValid = await verifyAdminPassword(savedPassword);
            if (isValid) {
                console.log('Saved password validated successfully');
                hideAdminLogin();
                initializeApp();
                return;
            } else {
                console.log('Saved password validation failed');
                // 密码无效，清除并显示登录界面
                adminPassword = null;
                clearSavedPassword();
            }
        } catch (e) {
            console.log('Password validation error, keeping saved password:', e);
            // 网络错误时仍然保留密码并尝试使用
            hideAdminLogin();
            initializeApp();
            return;
        }
    }
    
    // 显示登录界面
    showAdminLogin();
}

function toggleAdminPasswordVisibility() {
    const input = document.getElementById('adminPassword');
    const eyeIcon = document.getElementById('adminEyeIcon');
    
    if (input.type === 'password') {
        input.type = 'text';
        eyeIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />';
    } else {
        input.type = 'password';
        eyeIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />';
    }
}

// Admin Password Form Handler
document.addEventListener('DOMContentLoaded', function() {
    const adminForm = document.getElementById('adminPasswordForm');
    if (adminForm) {
        // 检查并恢复记住密码的勾选状态
        const hasSavedPassword = getSavedPassword() !== null;
        if (hasSavedPassword) {
            const rememberCheckbox = document.getElementById('rememberPassword');
            if (rememberCheckbox) {
                rememberCheckbox.checked = true;
            }
        }
        
        adminForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            
            const password = document.getElementById('adminPassword').value.trim();
            const remember = document.getElementById('rememberPassword').checked;
            const btn = document.getElementById('adminLoginBtn');
            const btnText = document.getElementById('adminBtnText');
            const btnLoading = document.getElementById('adminBtnLoading');
            
            if (!password) {
                document.getElementById('passwordError').textContent = '请输入管理密码';
                document.getElementById('passwordError').classList.remove('hidden');
                return;
            }
            
            btn.disabled = true;
            btnText.textContent = '验证中...';
            btnLoading.classList.remove('hidden');
            
            const success = await handleAdminLogin(password, remember);
            
            btn.disabled = false;
            btnText.textContent = '验证';
            btnLoading.classList.add('hidden');
            
            if (success) {
                document.getElementById('adminPassword').value = '';
            }
        });
    }
});

function getAuthHeaders() {
    const headers = {};
    if (adminPassword) {
        headers['X-Admin-Password'] = adminPassword;
    }
    return headers;
}

// --- Theme Management ---
function initTheme() {
    const isDark = localStorage.theme === 'dark' || 
        (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches);
    
    if (isDark) {
        document.documentElement.classList.add('dark');
    } else {
        document.documentElement.classList.remove('dark');
    }
    updateThemeIcons(isDark);
}

function toggleTheme() {
    const isDark = document.documentElement.classList.toggle('dark');
    localStorage.theme = isDark ? 'dark' : 'light';
    updateThemeIcons(isDark);
}

function updateThemeIcons(isDark) {
    const sun = document.getElementById('sunIcon');
    const moon = document.getElementById('moonIcon');
    if (isDark) {
        sun.classList.remove('hidden');
        moon.classList.add('hidden');
    } else {
        sun.classList.add('hidden');
        moon.classList.remove('hidden');
    }
}

document.getElementById('themeToggle').addEventListener('click', toggleTheme);

// --- Password Visibility ---
function togglePasswordVisibility() {
    const input = document.getElementById('client_secret');
    const eyeIcon = document.getElementById('eyeIcon');
    
    if (input.type === 'password') {
        input.type = 'text';
        eyeIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />';
    } else {
        input.type = 'password';
        eyeIcon.innerHTML = '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />';
    }
}

// --- Data Logic ---
const PLAN_LIMITS = { Free: 30, Starter: 280, Core: 750, Advanced: 1900, Max: 4200 };

function getStatusConfig(acc) {
    switch (acc.status) {
        case 'banned':
            return { text: '已封禁', class: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400', dot: 'bg-red-500' };
        case 'error':
            return { text: '异常', class: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400', dot: 'bg-yellow-500' };
        case 'cooling':
            return { text: '冷却中', class: 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400', dot: 'bg-orange-500' };
        case 'disabled':
            return { text: '已禁用', class: 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400', dot: 'bg-gray-500' };
        default:
            return { text: '正常', class: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400', dot: 'bg-green-500' };
    }
}

function getTokenStatusConfig(record) {
    switch (record.status) {
        case 'banned':
            return { text: '已封禁', class: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400', dot: 'bg-red-500' };
        case 'expired':
            return { text: '已过期', class: 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400', dot: 'bg-orange-500' };
        case 'disabled':
            return { text: '已禁用', class: 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400', dot: 'bg-gray-500' };
        default:
            return { text: '正常', class: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400', dot: 'bg-green-500' };
    }
}

function formatDate(dateStr) {
    if (!dateStr || dateStr.startsWith('0001')) return '-';
    const d = new Date(dateStr);
    return d.toLocaleDateString('zh-CN');
}

function formatLastUsed(dateStr) {
    if (!dateStr || dateStr.startsWith('0001')) return '从未';
    
    const date = new Date(dateStr);
    const now = new Date();
    const diff = now - date;
    
    // 转换为秒、分钟、小时、天
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    
    if (days > 0) {
        return `${days}天前`;
    } else if (hours > 0) {
        return `${hours}小时前`;
    } else if (minutes > 0) {
        return `${minutes}分钟前`;
    } else if (seconds > 0) {
        return `${seconds}秒前`;
    } else {
        return '刚刚';
    }
}

// 格式化积分刷新时间显示
function formatCreditRefresh(creditRefreshTimeStr) {
    if (!creditRefreshTimeStr || creditRefreshTimeStr.startsWith('0001')) {
        return { text: '未知', class: 'text-gray-400', detail: '' };
    }
    
    const refreshTime = new Date(creditRefreshTimeStr);
    const now = new Date();
    const diffMs = refreshTime - now;
    
    if (diffMs < 0) {
        // 已过期，需要刷新
        const daysPast = Math.floor(-diffMs / (1000 * 60 * 60 * 24));
        const hoursPast = Math.floor(-diffMs / (1000 * 60 * 60));
        
        if (daysPast > 0) {
            return {
                text: '需要刷新',
                class: 'text-red-600 dark:text-red-400',
                detail: `${daysPast}天前过期`
            };
        } else if (hoursPast > 0) {
            return {
                text: '需要刷新',
                class: 'text-red-600 dark:text-red-400',
                detail: `${hoursPast}小时前过期`
            };
        } else {
            return {
                text: '需要刷新',
                class: 'text-red-600 dark:text-red-400',
                detail: '刚过期'
            };
        }
    } else if (diffMs < 1000 * 60 * 60) {
        // 1小时内刷新
        const minutes = Math.floor(diffMs / (1000 * 60));
        return {
            text: `${minutes}分钟后`,
            class: 'text-orange-600 dark:text-orange-400',
            detail: '即将刷新'
        };
    } else if (diffMs < 1000 * 60 * 60 * 24) {
        // 24小时内刷新
        const hours = Math.floor(diffMs / (1000 * 60 * 60));
        return {
            text: `${hours}小时后`,
            class: 'text-yellow-600 dark:text-yellow-400',
            detail: '今日刷新'
        };
    } else {
        // 超过1天
        const days = Math.floor(diffMs / (1000 * 60 * 60 * 24));
        return {
            text: `${days}天后`,
            class: 'text-green-600 dark:text-green-400',
            detail: refreshTime.toLocaleDateString('zh-CN') + ' ' + refreshTime.toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'})
        };
    }
}

async function loadAccounts(isAutoRefresh = false) {
    try {
        const params = new URLSearchParams({
            page: currentState.page,
            size: currentState.size,
            status: currentState.category // map category to status param
        });

        const resp = await fetch(`${API_BASE}/accounts?${params}`, {
            headers: getAuthHeaders()
        });
        if (!resp.ok) throw new Error('Failed to fetch');
        
        const data = await resp.json();
        
        // Handle both old and new API response formats temporarily if needed, but we know it's new
        const items = data.items || [];
        const total = data.total || 0;

        currentState.items = items;
        currentState.total = total;
        
        // Retain selection if items still exist
        const newSet = new Set();
        items.forEach(item => {
            if (currentState.selectedIds.has(item.id)) {
                newSet.add(item.id);
            }
        });
        currentState.selectedIds = newSet;

        renderAccounts(items);
        updatePaginationUI();
        updateTabsUI();
        updateBatchUI();
        
        if (data.stats) {
            updateStatsUI(data.stats);
        }
    } catch (e) {
        if (!isAutoRefresh) console.error("Failed to load accounts", e);
    }
}

function updateStatsUI(stats) {
    if (!stats) return;
    document.getElementById('stat-total-accounts').textContent = stats.total_accounts;
    document.getElementById('stat-active-accounts').textContent = stats.active_accounts;
    document.getElementById('stat-cooling-accounts').textContent = stats.cooling_accounts || 0;
    document.getElementById('stat-banned-accounts').textContent = stats.banned_accounts;
    document.getElementById('stat-error-accounts').textContent = stats.error_accounts;
    document.getElementById('stat-disabled-accounts').textContent = stats.disabled_accounts || 0;
    document.getElementById('stat-today-usage').textContent = stats.today_usage.toFixed(2);
    document.getElementById('stat-total-usage').textContent = stats.total_usage.toFixed(2);
}

function renderAccounts(accounts) {
    const tbody = document.getElementById('accountList');
    const emptyState = document.getElementById('emptyState');
    const tableContainer = document.getElementById('tableContainer');
    const paginationContainer = document.getElementById('paginationContainer');
    const selectAll = document.getElementById('selectAll');
    
    if (accounts.length === 0) {
        tbody.innerHTML = '';
        tableContainer.classList.add('hidden');
        paginationContainer.classList.add('hidden');

        emptyState.classList.remove('hidden');
        emptyState.classList.add('flex');
        selectAll.checked = false;
        selectAll.disabled = true;
        return;
    }

    tableContainer.classList.remove('hidden');
    paginationContainer.classList.remove('hidden');

    emptyState.classList.add('hidden');
    emptyState.classList.remove('flex');
    
    selectAll.disabled = false;
    selectAll.checked = accounts.length > 0 && accounts.every(a => currentState.selectedIds.has(a.id));

    const html = accounts.map(acc => {
        const status = getStatusConfig(acc);
        const limit = PLAN_LIMITS[acc.plan_type] || 30;
        const subDate = formatDate(acc.subscription_start_date);
        const emailOrId = acc.email ? acc.email : acc.client_id;
        const shortId = acc.client_id.length > 12 ? acc.client_id.substring(0, 12) + '...' : acc.client_id;
        const usagePercent = Math.min((acc.daily_used / limit) * 100, 100);
        const isSelected = currentState.selectedIds.has(acc.id);
        const lastUsedText = formatLastUsed(acc.last_used);
        
        // 格式化Token过期时间
        const formatTokenExpiry = (tokenExpiryStr) => {
            if (!tokenExpiryStr || tokenExpiryStr.startsWith('0001')) {
                return { text: '未知', class: 'text-gray-400', detail: '' };
            }
            
            const expiryDate = new Date(tokenExpiryStr);
            const now = new Date();
            const diffMs = expiryDate - now;
            
            if (diffMs < 0) {
                // 已过期
                const daysPast = Math.floor(-diffMs / (1000 * 60 * 60 * 24));
                return {
                    text: '已过期',
                    class: 'text-red-600 dark:text-red-400',
                    detail: `${daysPast}天前过期`
                };
            } else if (diffMs < 1000 * 60 * 60) {
                // 1小时内过期
                const minutes = Math.floor(diffMs / (1000 * 60));
                return {
                    text: `${minutes}分钟后`,
                    class: 'text-red-500 dark:text-red-400',
                    detail: '即将过期'
                };
            } else if (diffMs < 1000 * 60 * 60 * 24) {
                // 24小时内过期
                const hours = Math.floor(diffMs / (1000 * 60 * 60));
                return {
                    text: `${hours}小时后`,
                    class: 'text-orange-600 dark:text-orange-400',
                    detail: '今日过期'
                };
            } else {
                // 超过1天
                const days = Math.floor(diffMs / (1000 * 60 * 60 * 24));
                return {
                    text: `${days}天后`,
                    class: 'text-green-600 dark:text-green-400',
                    detail: expiryDate.toLocaleDateString('zh-CN')
                };
            }
        };
        
        const tokenExpiry = formatTokenExpiry(acc.token_expiry);
        
        return `
        <tr class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors ${isSelected ? 'bg-blue-50 dark:bg-blue-900/10' : ''}">
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <input type="checkbox" onchange="toggleSelect(${acc.id})" ${isSelected ? 'checked' : ''} class="rounded border-gray-300 dark:border-gray-600 text-primary focus:ring-primary h-4 w-4 mx-auto">
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <div class="text-sm font-medium text-gray-900 dark:text-white">${acc.id}</div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <div class="flex items-center justify-center text-center">
                    <div>
                        <div class="text-sm font-medium text-gray-900 dark:text-white">${emailOrId}</div>
                        <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mt-0.5" title="${acc.client_id}">ID: ${shortId}</div>
                    </div>
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <div class="text-sm text-gray-900 dark:text-white font-medium">${acc.plan_type}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">自: ${subDate}</div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap">
                <div class="w-full max-w-[140px] mx-auto">
                    <div class="flex justify-end gap-1 text-xs mb-1">
                        <span class="text-gray-900 dark:text-white font-medium">${acc.daily_used.toFixed(2)}</span>
                        <span class="text-gray-500">/ ${limit}</span>
                    </div>
                    <div class="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-1.5 overflow-hidden">
                        <div class="bg-primary h-1.5 rounded-full" style="width: ${usagePercent}%"></div>
                    </div>
                    <div class="text-[10px] text-gray-400 mt-1">总计: ${acc.total_used.toFixed(2)}</div>
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <div class="text-sm">
                    <span class="${lastUsedText === '从未' ? 'text-gray-400' : 'text-gray-600 dark:text-gray-300'}">${lastUsedText}</span>
                    ${acc.last_used && !acc.last_used.startsWith('0001') ?
                        `<div class="text-[10px] text-gray-400 mt-0.5">${new Date(acc.last_used).toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'})}</div>`
                        : ''}
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <div class="text-sm">
                    <span class="${tokenExpiry.class} font-medium">${tokenExpiry.text}</span>
                    ${tokenExpiry.detail ? `<div class="text-[10px] text-gray-400 mt-0.5">${tokenExpiry.detail}</div>` : ''}
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <div class="text-sm">
                    <span class="${formatCreditRefresh(acc.credit_refresh_time).class} font-medium">${formatCreditRefresh(acc.credit_refresh_time).text}</span>
                    ${formatCreditRefresh(acc.credit_refresh_time).detail ? `<div class="text-[10px] text-gray-400 mt-0.5">${formatCreditRefresh(acc.credit_refresh_time).detail}</div>` : ''}
                </div>
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center">
                <span class="px-2.5 py-0.5 inline-flex items-center text-xs font-medium rounded-full ${status.class}">
                    <span class="w-1.5 h-1.5 rounded-full ${status.dot} mr-1.5"></span>
                    ${status.text}
                </span>
                ${acc.status === 'cooling' && acc.cooling_until && !acc.cooling_until.startsWith('0001') ?
                    (() => {
                        const coolingDate = new Date(acc.cooling_until);
                        const now = new Date();
                        const diffMs = coolingDate - now;
                        
                        if (diffMs > 0) {
                            const hours = Math.floor(diffMs / (1000 * 60 * 60));
                            const minutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));
                            
                            let timeText = '';
                            if (hours > 0) {
                                timeText = `${hours}小时${minutes}分钟后解除`;
                            } else {
                                timeText = `${minutes}分钟后解除`;
                            }
                            
                            return `<div class="text-[10px] text-orange-500 dark:text-orange-400 mt-1" title="${coolingDate.toLocaleString('zh-CN')}">
                                <svg xmlns="http://www.w3.org/2000/svg" class="inline h-3 w-3 mr-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
                                </svg>
                                ${timeText}
                            </div>`;
                        } else {
                            return `<div class="text-[10px] text-orange-500 dark:text-orange-400 mt-1">即将解除</div>`;
                        }
                    })()
                    : acc.ban_reason ? `<div class="text-[10px] text-red-500 dark:text-red-400 mt-1 max-w-[120px] truncate mx-auto" title="${acc.ban_reason}">${acc.ban_reason}</div>` : ''}
            </td>
            <td class="px-6 py-4 whitespace-nowrap text-center text-sm font-medium">
                <div class="flex justify-center gap-3">
                    <button onclick="toggleAccount(${acc.id})" class="text-primary hover:text-primary-hover transition-colors" title="${acc.is_active ? '禁用' : '启用'}">
                        ${acc.is_active
                            ? '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>'
                            : '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" /><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>'
                        }
                    </button>
                    <button onclick="deleteAccount(${acc.id})" class="text-red-500 hover:text-red-700 transition-colors" title="删除">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                    </button>
                </div>
            </td>
        </tr>`;
    }).join('');

    // Optimization: Only update if content changed
    if (tbody.innerHTML !== html) {
        tbody.innerHTML = html;
    }
}

// --- Interactions ---

function switchCategory(cat) {
    currentState.category = cat;
    currentState.page = 1;
    currentState.selectedIds.clear();
    loadAccounts();
}

function updateTabsUI() {
    ['normal', 'banned', 'cooling', 'disabled', 'error'].forEach(cat => {
        const btn = document.getElementById(`tab-${cat}`);
        if (currentState.category === cat) {
            btn.className = "px-3 py-1.5 text-xs font-medium rounded-md transition-all bg-white dark:bg-gray-600 text-gray-900 dark:text-white shadow-sm";
        } else {
            btn.className = "px-3 py-1.5 text-xs font-medium rounded-md transition-all text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white";
        }
    });
}

function changePage(delta) {
    const newPage = currentState.page + delta;
    if (newPage > 0 && newPage <= Math.ceil(currentState.total / currentState.size)) {
        currentState.page = newPage;
        loadAccounts();
    }
}

function updatePaginationUI() {
    const totalPages = Math.ceil(currentState.total / currentState.size);
    document.getElementById('pageStart').textContent = currentState.total === 0 ? 0 : (currentState.page - 1) * currentState.size + 1;
    document.getElementById('pageEnd').textContent = Math.min(currentState.page * currentState.size, currentState.total);
    document.getElementById('totalItems').textContent = currentState.total;
    
    document.getElementById('prevPage').disabled = currentState.page <= 1;
    document.getElementById('nextPage').disabled = currentState.page >= totalPages;
}

function toggleSelectAll() {
    const selectAll = document.getElementById('selectAll');
    if (selectAll.checked) {
        currentState.items.forEach(item => currentState.selectedIds.add(item.id));
    } else {
        currentState.selectedIds.clear();
    }
    renderAccounts(currentState.items); // re-render to show selection state
    updateBatchUI();
}

function toggleSelect(id) {
    if (currentState.selectedIds.has(id)) {
        currentState.selectedIds.delete(id);
    } else {
        currentState.selectedIds.add(id);
    }
    renderAccounts(currentState.items);
    updateBatchUI();
}

function updateBatchUI() {
    const batchActions = document.getElementById('batchActions');
    const countSpan = document.getElementById('selectedCount');
    const count = currentState.selectedIds.size;
    
    // 始终显示批量操作区域
    batchActions.classList.remove('hidden');
    batchActions.classList.add('flex');
    
    // 根据当前tab和选中状态显示不同的按钮
    const buttonsHtml = getBatchButtonsHtml(currentState.category, count);
    
    // 更新按钮区域内容
    if (count > 0) {
        countSpan.textContent = `${count} 选中`;
        countSpan.classList.remove('hidden');
    } else {
        countSpan.classList.add('hidden');
    }
    
    // 更新按钮容器
    const buttonsContainer = document.getElementById('batchButtonsContainer');
    if (buttonsContainer) {
        buttonsContainer.innerHTML = buttonsHtml;
    }
}

function getBatchButtonsHtml(category, selectedCount) {
    // 根据是否有选中数据，决定使用哪个函数
    const moveHandler = selectedCount > 0 ? 'batchMove' : 'oneClickMove';
    const deleteHandler = selectedCount > 0 ? 'batchDelete(false)' : 'batchDelete(true)';
    
    // 统一的按钮布局，只隐藏当前tab对应的按钮
    return `
        <button onclick="${moveHandler}('normal')" class="px-3 py-1.5 bg-green-600 text-white text-xs font-medium rounded-md hover:bg-green-700 transition-colors ${category === 'normal' ? 'hidden' : ''}">
            移至正常
        </button>
        <button onclick="${moveHandler}('cooling')" class="px-3 py-1.5 bg-orange-600 text-white text-xs font-medium rounded-md hover:bg-orange-700 transition-colors ${category === 'cooling' ? 'hidden' : ''}">
            移至冷却
        </button>
        <button onclick="${moveHandler}('disabled')" class="px-3 py-1.5 bg-gray-600 text-white text-xs font-medium rounded-md hover:bg-gray-700 transition-colors ${category === 'disabled' ? 'hidden' : ''}">
            移至禁用
        </button>
        <button onclick="${moveHandler}('banned')" class="px-3 py-1.5 bg-red-600 text-white text-xs font-medium rounded-md hover:bg-red-700 transition-colors ${category === 'banned' ? 'hidden' : ''}">
            移至封禁
        </button>
        <button onclick="${moveHandler}('error')" class="px-3 py-1.5 bg-yellow-600 text-white text-xs font-medium rounded-md hover:bg-yellow-700 transition-colors ${category === 'error' ? 'hidden' : ''}">
            移至异常
        </button>
        <div class="border-l border-gray-300 dark:border-gray-600 mx-2 h-6"></div>
        <button onclick="${deleteHandler}" class="px-3 py-1.5 bg-red-700 text-white text-xs font-medium rounded-md hover:bg-red-800 transition-colors flex items-center gap-1">
            <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            ${selectedCount > 0 ? '删除选中' : '删除全部'}
        </button>
    `;
}

async function oneClickMove(targetCategory) {
    // 一键移动当前分类的所有账号到目标分类
    if (currentState.category === targetCategory) {
        alert('当前已在目标分类中');
        return;
    }
    
    const confirmMsg = `确定要将当前分类"${getCategoryName(currentState.category)}"的所有账号移至"${getCategoryName(targetCategory)}"吗？`;
    if (!confirm(confirmMsg)) return;
    
    try {
        const resp = await fetch(`${API_BASE}/accounts/batch/move-all`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify({
                from_status: currentState.category,
                to_status: targetCategory
            })
        });
        
        if (!resp.ok) throw new Error('One-click move failed');
        
        const result = await resp.json();
        alert(`成功移动 ${result.moved_count || 0} 个账号`);
        
        // 刷新当前页面
        loadAccounts();
    } catch (e) {
        alert('一键操作失败: ' + e.message);
    }
}

function getCategoryName(category) {
    const names = {
        'normal': '正常',
        'cooling': '冷却',
        'banned': '封禁',
        'disabled': '禁用',
        'error': '异常'
    };
    return names[category] || category;
}

async function batchMove(category) {
    if (currentState.selectedIds.size === 0) return;
    
    const ids = Array.from(currentState.selectedIds);
    try {
        const resp = await fetch(`${API_BASE}/accounts/batch/category`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify({ ids, status: category }) // send as status
        });
        
        if (!resp.ok) throw new Error('Batch update failed');
        
        currentState.selectedIds.clear();
        loadAccounts();
    } catch (e) {
        alert('批量操作失败');
    }
}

// 批量删除功能
async function batchDelete(deleteAll = false) {
    let confirmMsg;
    let requestData;
    
    if (deleteAll) {
        // 删除当前分类的所有账号
        confirmMsg = `确定要删除当前分类"${getCategoryName(currentState.category)}"的所有账号吗？此操作不可逆转！`;
        requestData = {
            delete_all: true,
            status: currentState.category
        };
    } else {
        // 删除选中的账号
        if (currentState.selectedIds.size === 0) {
            alert('请先选择要删除的账号');
            return;
        }
        confirmMsg = `确定要删除选中的 ${currentState.selectedIds.size} 个账号吗？此操作不可逆转！`;
        requestData = {
            ids: Array.from(currentState.selectedIds)
        };
    }
    
    if (!confirm(confirmMsg)) return;
    
    try {
        const resp = await fetch(`${API_BASE}/accounts/batch/delete`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify(requestData)
        });
        
        if (!resp.ok) {
            const err = await resp.json();
            throw new Error(err.error || 'Delete failed');
        }
        
        const result = await resp.json();
        alert(`成功删除 ${result.deleted_count || 0} 个账号`);
        
        // 清空选择并刷新页面
        currentState.selectedIds.clear();
        loadAccounts();
    } catch (e) {
        alert('批量删除失败: ' + e.message);
    }
}

// 批量刷新Token功能
async function batchRefreshToken(refreshAll = false) {
    let confirmMsg;
    let requestData;
    
    if (refreshAll) {
        confirmMsg = "确定要刷新所有正常账号的Token吗？此操作可能需要较长时间。";
        requestData = { all: true };
    } else {
        if (currentState.selectedIds.size === 0) {
            alert('请先选择要刷新Token的账号');
            return;
        }
        confirmMsg = `确定要刷新选中的 ${currentState.selectedIds.size} 个账号的Token吗？`;
        requestData = { ids: Array.from(currentState.selectedIds) };
    }
    
    if (!confirm(confirmMsg)) return;
    
    try {
        // 显示进度弹窗
        showRefreshProgressModal();
        addRefreshProgressLog('开始批量刷新Token...', 'info');
        
        const resp = await fetch(`${API_BASE}/accounts/batch/refresh-token`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify(requestData)
        });
        
        if (!resp.ok) {
            const err = await resp.json();
            throw new Error(err.error || 'Unknown error');
        }

        // 处理流式响应
        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let successCount = 0;
        let failCount = 0;
        let total = 0;

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop(); // 保留不完整的行

            for (const line of lines) {
                if (!line.trim() || !line.startsWith('data: ')) continue;
                
                try {
                    const jsonStr = line.substring(6); // 移除 "data: " 前缀
                    const event = JSON.parse(jsonStr);
                    
                    switch (event.type) {
                        case 'start':
                            total = event.total;
                            updateRefreshProgress(0, total, '开始刷新');
                            addRefreshProgressLog(`准备刷新 ${total} 个账号的Token`, 'info');
                            break;
                        
                        case 'success':
                            successCount++;
                            updateRefreshProgress(successCount + failCount, total, '刷新中');
                            addRefreshProgressLog(`✓ [${event.index}/${total}] 刷新成功: ${event.account_id}${event.email ? ` (${event.email})` : ''}`, 'success');
                            break;
                        
                        case 'error':
                            failCount++;
                            updateRefreshProgress(successCount + failCount, total, '刷新中');
                            addRefreshProgressLog(`✗ [${event.index}/${total}] ${event.message} (${event.account_id})`, 'error');
                            break;
                        
                        case 'complete':
                            updateRefreshProgress(total, total, '完成');
                            addRefreshProgressLog(`批量刷新完成！成功 ${event.success} 个，失败 ${event.fail} 个`, 'info');
                            showRefreshProgressSummary(event.success, event.fail);
                            break;
                    }
                } catch (e) {
                    console.error('解析事件失败:', e, line);
                }
            }
        }
        
        return true;
    } catch (e) {
        addRefreshProgressLog(`错误: ${e.message}`, 'error');
        document.getElementById('refreshProgressCloseBtn').classList.remove('hidden');
        return false;
    }
}

// 刷新进度弹窗管理
function showRefreshProgressModal() {
    document.getElementById('refreshProgressModal').classList.remove('hidden');
    document.getElementById('refreshProgressLog').innerHTML = '';
    document.getElementById('refreshProgressBar').style.width = '0%';
    document.getElementById('refreshProgressText').textContent = '准备中...';
    document.getElementById('refreshProgressCount').textContent = '0/0';
    document.getElementById('refreshProgressSummary').classList.add('hidden');
    document.getElementById('refreshProgressCloseBtn').classList.add('hidden');
}

function closeRefreshProgressModal() {
    document.getElementById('refreshProgressModal').classList.add('hidden');
    loadAccounts(); // 刷新账号列表
    currentState.selectedIds.clear(); // 清空选择
    updateBatchUI(); // 更新批量操作UI
}

function addRefreshProgressLog(message, type = 'info') {
    const log = document.getElementById('refreshProgressLog');
    const colors = {
        info: 'text-gray-600 dark:text-gray-400',
        success: 'text-green-600 dark:text-green-400',
        error: 'text-red-600 dark:text-red-400',
        warning: 'text-yellow-600 dark:text-yellow-400'
    };
    
    const entry = document.createElement('div');
    entry.className = colors[type] || colors.info;
    entry.textContent = message;
    log.appendChild(entry);
    
    // 自动滚动到底部
    log.scrollTop = log.scrollHeight;
}

function updateRefreshProgress(current, total, text) {
    const percentage = total > 0 ? (current / total) * 100 : 0;
    document.getElementById('refreshProgressBar').style.width = `${percentage}%`;
    document.getElementById('refreshProgressText').textContent = text;
    document.getElementById('refreshProgressCount').textContent = `${current}/${total}`;
}

function showRefreshProgressSummary(success, fail) {
    document.getElementById('refreshSummarySuccess').textContent = success;
    document.getElementById('refreshSummaryFail').textContent = fail;
    document.getElementById('refreshProgressSummary').classList.remove('hidden');
    document.getElementById('refreshProgressCloseBtn').classList.remove('hidden');
}

async function addAccount(data) {
    const btn = document.getElementById('submitBtn');
    const btnText = document.getElementById('btnText');
    const btnLoading = document.getElementById('btnLoading');
    
    btn.disabled = true;
    btnLoading.classList.remove('hidden');

    try {
        // 生成模式使用流式传输
        if (data.generate_mode) {
            btnText.textContent = '生成中...';
            
            const resp = await fetch(`${API_BASE}/accounts`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    ...getAuthHeaders()
                },
                body: JSON.stringify(data)
            });
            
            if (!resp.ok) {
                const err = await resp.json();
                throw new Error(err.error || 'Unknown error');
            }

            // 处理流式响应
            const reader = resp.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            let successCount = 0;
            let failCount = 0;
            let total = 0;
            let firstSuccessAccount = null; // 记录第一个成功的账号信息
            let isProgressShown = false;

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop(); // 保留不完整的行

                for (const line of lines) {
                    if (!line.trim() || !line.startsWith('data: ')) continue;
                    
                    try {
                        const jsonStr = line.substring(6); // 移除 "data: " 前缀
                        const event = JSON.parse(jsonStr);
                        
                        switch (event.type) {
                            case 'start':
                                total = event.total;
                                // 如果是单个账号生成，不显示进度窗口
                                if (total > 1) {
                                    showProgressModal();
                                    updateProgress(0, total, '开始生成');
                                    addProgressLog('开始批量生成凭证...', 'info');
                                    addProgressLog(`准备生成 ${total} 个凭证`, 'info');
                                    isProgressShown = true;
                                }
                                break;
                            
                            case 'success':
                                successCount++;
                                // 记录第一个成功的账号信息（单个生成时使用）
                                if (!firstSuccessAccount && event.email) {
                                    firstSuccessAccount = {
                                        email: event.email,
                                        plan: event.plan,
                                        token_expiry: event.token_expiry,
                                        subscription_start_date: event.subscription_start_date
                                    };
                                }
                                
                                if (isProgressShown) {
                                    updateProgress(successCount + failCount, total, '生成中');
                                    const action = event.action === 'created' ? '创建' : '更新';
                                    addProgressLog(`✓ [${event.index}/${total}] ${action}成功: ${event.email} (${event.plan})`, 'success');
                                }
                                break;
                            
                            case 'error':
                                failCount++;
                                if (isProgressShown) {
                                    updateProgress(successCount + failCount, total, '生成中');
                                    const clientInfo = event.client_id ? ` (${event.client_id})` : '';
                                    addProgressLog(`✗ [${event.index}/${total}] ${event.message}${clientInfo}`, 'error');
                                }
                                break;
                            
                            case 'complete':
                                if (isProgressShown) {
                                    // 批量生成完成
                                    updateProgress(total, total, '完成');
                                    addProgressLog(`批量生成完成！成功 ${event.success} 个，失败 ${event.fail} 个`, 'info');
                                    showProgressSummary(event.success, event.fail);
                                } else {
                                    // 单个账号生成完成
                                    if (firstSuccessAccount) {
                                        // 显示账号信息弹窗
                                        showAccountInfoModal(firstSuccessAccount);
                                    } else if (failCount > 0) {
                                        // 生成失败
                                        showToast('账号生成失败', 'error');
                                    }
                                }
                                break;
                        }
                    } catch (e) {
                        console.error('解析事件失败:', e, line);
                    }
                }
            }
            
            return true;
        } else {
            // 凭证模式使用普通请求
            btnText.textContent = '添加中...';
            
            const resp = await fetch(`${API_BASE}/accounts`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    ...getAuthHeaders()
                },
                body: JSON.stringify(data)
            });
            
            if (!resp.ok) {
                const err = await resp.json();
                throw new Error(err.error || 'Unknown error');
            }

            loadAccounts();
            return true;
        }
    } catch (e) {
        if (data.generate_mode) {
            showToast(`生成失败: ${e.message}`, 'error');
        } else {
            alert('操作失败: ' + e.message);
        }
        return false;
    } finally {
        btn.disabled = false;
        btnText.textContent = currentAddMode === 'credential' ? '添加账号' : '批量生成';
        btnLoading.classList.add('hidden');
    }
}

async function deleteAccount(id) {
    if (!confirm('确定要删除此账号吗？')) return;
    try {
        await fetch(`${API_BASE}/accounts/${id}`, {
            method: 'DELETE',
            headers: getAuthHeaders()
        });
        loadAccounts();
    } catch (e) {
        alert('删除失败');
    }
}

async function toggleAccount(id) {
    try {
        await fetch(`${API_BASE}/accounts/${id}/toggle`, {
            method: 'POST',
            headers: getAuthHeaders()
        });
        loadAccounts();
    } catch (e) {
        alert('操作失败');
    }
}

// --- Progress Modal Management ---
function showProgressModal() {
    document.getElementById('progressModal').classList.remove('hidden');
    document.getElementById('progressLog').innerHTML = '';
    document.getElementById('progressBar').style.width = '0%';
    document.getElementById('progressText').textContent = '准备中...';
    document.getElementById('progressCount').textContent = '0/0';
    document.getElementById('progressSummary').classList.add('hidden');
    document.getElementById('progressCloseBtn').classList.add('hidden');
}

function closeProgressModal() {
    document.getElementById('progressModal').classList.add('hidden');
    loadAccounts(); // 刷新账号列表
}

function addProgressLog(message, type = 'info') {
    const log = document.getElementById('progressLog');
    const colors = {
        info: 'text-gray-600 dark:text-gray-400',
        success: 'text-green-600 dark:text-green-400',
        error: 'text-red-600 dark:text-red-400',
        warning: 'text-yellow-600 dark:text-yellow-400'
    };
    
    const entry = document.createElement('div');
    entry.className = colors[type] || colors.info;
    entry.textContent = message;
    log.appendChild(entry);
    
    // 自动滚动到底部
    log.scrollTop = log.scrollHeight;
}

function updateProgress(current, total, text) {
    const percentage = total > 0 ? (current / total) * 100 : 0;
    document.getElementById('progressBar').style.width = `${percentage}%`;
    document.getElementById('progressText').textContent = text;
    document.getElementById('progressCount').textContent = `${current}/${total}`;
}

function showProgressSummary(success, fail) {
    document.getElementById('summarySuccess').textContent = success;
    document.getElementById('summaryFail').textContent = fail;
    document.getElementById('progressSummary').classList.remove('hidden');
    document.getElementById('progressCloseBtn').classList.remove('hidden');
}

// --- Add Mode Management ---
let currentAddMode = 'credential'; // 'credential' or 'generate'

function switchAddMode(mode) {
    currentAddMode = mode;
    const credentialBtn = document.getElementById('mode-credential');
    const generateBtn = document.getElementById('mode-generate');
    const credentialFields = document.getElementById('credentialFields');
    const generateFields = document.getElementById('generateFields');
    const submitBtn = document.getElementById('btnText');
    
    if (mode === 'credential') {
        credentialBtn.classList.add('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        credentialBtn.classList.remove('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        generateBtn.classList.remove('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        generateBtn.classList.add('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        credentialFields.classList.remove('hidden');
        generateFields.classList.add('hidden');
        submitBtn.textContent = '添加账号';
    } else {
        generateBtn.classList.add('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        generateBtn.classList.remove('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        credentialBtn.classList.remove('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        credentialBtn.classList.add('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        credentialFields.classList.add('hidden');
        generateFields.classList.remove('hidden');
        submitBtn.textContent = '批量生成';
    }
}

// Form Handler
document.getElementById('addForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    
    let data;
    
    if (currentAddMode === 'credential') {
        const accessToken = document.getElementById('credential_access_token').value.trim();
        const refreshToken = document.getElementById('credential_refresh_token').value.trim();
        const proxy = document.getElementById('proxy').value.trim();

        if (!accessToken && !refreshToken) {
            alert('请填写 Access Token 或 Refresh Token（至少一个）');
            return;
        }

        data = {
            proxy: proxy,
            generate_mode: false
        };

        // 提交用户填写的所有token字段，后端会根据优先级处理
        if (accessToken) {
            data.access_token = accessToken;
        }
        if (refreshToken) {
            data.refresh_token = refreshToken;
        }
    } else {
        const masterAccessToken = document.getElementById('generate_access_token').value.trim();
        const masterRefreshToken = document.getElementById('generate_refresh_token').value.trim();
        const proxy = document.getElementById('proxy').value.trim();

        if (!masterAccessToken && !masterRefreshToken) {
            alert('请填写 Master Access Token 或 Master Refresh Token（至少一个）');
            return;
        }

        data = {
            proxy: proxy,
            generate_mode: true
        };

        // 提交用户填写的所有token字段，后端会根据优先级处理
        if (masterAccessToken) {
            data.access_token = masterAccessToken;
        }
        if (masterRefreshToken) {
            data.refresh_token = masterRefreshToken;
        }
    }

    if (await addAccount(data)) {
        e.target.reset();
        if (currentAddMode === 'credential') {
            document.getElementById('credential_refresh_token').focus();
        } else {
            document.getElementById('generate_refresh_token').focus();
        }
    }
});

// Initialization function for after admin login
function initializeApp() {
    loadAccounts();
    
    // Auto Refresh
    if (autoRefreshTimer) {
        clearInterval(autoRefreshTimer);
    }
    autoRefreshTimer = setInterval(() => {
        loadAccounts(true);
    }, REFRESH_INTERVAL);
}

// --- Token Management ---
let currentMainView = 'pool'; // 'pool' or 'token'
let tokenRecords = [];
let generationTasks = [];

function switchMainView(view) {
    currentMainView = view;
    const poolView = document.getElementById('poolView');
    const tokenView = document.getElementById('tokenView');
    const poolBtn = document.getElementById('view-pool');
    const tokenBtn = document.getElementById('view-token');
    
    if (view === 'pool') {
        poolView.classList.remove('hidden');
        tokenView.classList.add('hidden');
        
        poolBtn.classList.add('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        poolBtn.classList.remove('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        tokenBtn.classList.remove('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        tokenBtn.classList.add('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        
        loadAccounts();
    } else {
        poolView.classList.add('hidden');
        tokenView.classList.remove('hidden');
        
        tokenBtn.classList.add('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        tokenBtn.classList.remove('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        poolBtn.classList.remove('bg-white', 'dark:bg-gray-600', 'text-gray-900', 'dark:text-white', 'shadow-sm');
        poolBtn.classList.add('text-gray-500', 'hover:text-gray-900', 'dark:text-gray-400', 'dark:hover:text-white');
        
        loadTokenData();
    }
}

async function loadTokenData() {
    await Promise.all([
        loadTokenRecords(),
        loadGenerationTasks(),
        loadPoolStatus()
    ]);
}

async function loadTokenRecords() {
    try {
        const resp = await fetch(`${API_BASE}/tokens`, {
            headers: getAuthHeaders()
        });
        if (!resp.ok) throw new Error('Failed to fetch token records');
        
        const data = await resp.json();
        tokenRecords = data.items || [];
        renderTokenRecords(tokenRecords);
    } catch (e) {
        console.error("Failed to load token records", e);
    }
}

async function loadGenerationTasks() {
    try {
        const resp = await fetch(`${API_BASE}/tokens/tasks`, {
            headers: getAuthHeaders()
        });
        if (!resp.ok) throw new Error('Failed to fetch generation tasks');
        
        const data = await resp.json();
        generationTasks = data.items || [];
        renderGenerationTasks(generationTasks);
    } catch (e) {
        console.error("Failed to load generation tasks", e);
    }
}

async function loadPoolStatus() {
    try {
        const resp = await fetch(`${API_BASE}/tokens/pool-status`, {
            headers: getAuthHeaders()
        });
        if (!resp.ok) throw new Error('Failed to fetch pool status');
        
        const data = await resp.json();
        updateTokenStatsUI(data);
    } catch (e) {
        console.error("Failed to load pool status", e);
    }
}

function updateTokenStatsUI(stats) {
    document.getElementById('token-stat-active').textContent = stats.active_tokens || 0;
    document.getElementById('token-stat-normal').textContent = stats.normal_accounts || 0;
    document.getElementById('token-stat-running').textContent = stats.running_tasks || 0;
}

function renderTokenRecords(records) {
    const tbody = document.getElementById('tokenList');
    const emptyState = document.getElementById('tokenEmptyState');
    
    if (records.length === 0) {
        tbody.innerHTML = '';
        emptyState.classList.remove('hidden');
        emptyState.classList.add('flex');
        return;
    }
    
    emptyState.classList.add('hidden');
    emptyState.classList.remove('flex');
    
    const html = records.map(record => {
        // 格式化订阅日期
        const subDate = record.subscription_start_date && !record.subscription_start_date.startsWith('0001')
            ? new Date(record.subscription_start_date).toLocaleDateString('zh-CN')
            : '-';
        
        // 格式化Token过期时间
        const tokenExpiryDate = record.token_expiry && !record.token_expiry.startsWith('0001')
            ? new Date(record.token_expiry)
            : null;
        
        let tokenStatusClass, tokenStatusText;
        if (!tokenExpiryDate) {
            tokenStatusClass = 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400';
            tokenStatusText = '未知';
        } else {
            const now = new Date();
            const hoursUntilExpiry = (tokenExpiryDate - now) / (1000 * 60 * 60);
            
            if (hoursUntilExpiry < 0) {
                tokenStatusClass = 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400';
                tokenStatusText = '已过期';
            } else if (hoursUntilExpiry < 1) {
                tokenStatusClass = 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400';
                tokenStatusText = `${Math.floor(hoursUntilExpiry * 60)}分钟后过期`;
            } else if (hoursUntilExpiry < 24) {
                tokenStatusClass = 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400';
                tokenStatusText = `${Math.floor(hoursUntilExpiry)}小时后过期`;
            } else {
                tokenStatusClass = 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400';
                tokenStatusText = `${Math.floor(hoursUntilExpiry / 24)}天后过期`;
            }
        }
        
        return `
        <tr class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
            <td class="px-6 py-4">
                <div>
                    <div class="text-sm font-medium text-gray-900 dark:text-white">${record.email || '未知邮箱'}</div>
                    <div class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">${record.description || `令牌 #${record.id}`}</div>
                </div>
            </td>
            <td class="px-6 py-4 text-center">
                <div>
                    <div class="text-sm font-medium ${record.plan_type === 'Starter' ? 'text-blue-600 dark:text-blue-400' : record.plan_type === 'Core' ? 'text-purple-600 dark:text-purple-400' : record.plan_type === 'Advanced' ? 'text-orange-600 dark:text-orange-400' : record.plan_type === 'Max' ? 'text-red-600 dark:text-red-400' : 'text-gray-600 dark:text-gray-400'}">${record.plan_type || 'Free'}</div>
                </div>
            </td>
            <td class="px-6 py-4 text-center">
                <span class="text-sm text-gray-600 dark:text-gray-400">${subDate}</span>
            </td>
            <td class="px-6 py-4 text-center">
                <div class="space-y-1">
                    <div>
                        <span class="px-2 py-0.5 inline-flex items-center text-xs font-medium rounded-full ${getTokenStatusConfig(record).class}">
                            <span class="w-1.5 h-1.5 rounded-full ${getTokenStatusConfig(record).dot} mr-1.5"></span>
                            ${getTokenStatusConfig(record).text}
                        </span>
                    </div>
                    <div>
                        <span class="px-2 py-0.5 inline-flex text-xs rounded-full ${tokenStatusClass}">
                            ${tokenStatusText}
                        </span>
                    </div>
                </div>
            </td>
            <td class="px-6 py-4 text-center">
                <div class="text-sm">
                    <span class="text-gray-900 dark:text-white font-medium">${record.generated_count || 0}</span>
                    <span class="text-gray-500">/</span>
                    <span class="text-gray-600 dark:text-gray-400">${record.threshold}</span>
                    <div class="text-xs text-gray-500 dark:text-gray-400 mt-0.5">批次: ${record.generate_batch}</div>
                </div>
            </td>
            <td class="px-6 py-4 text-center">
                <div class="text-sm">
                    <span class="text-green-600 dark:text-green-400">${record.total_success || 0}</span>
                    <span class="text-gray-500">/</span>
                    <span class="text-red-600 dark:text-red-400">${record.total_fail || 0}</span>
                </div>
            </td>
            <td class="px-6 py-4 text-center">
                <button onclick="quickToggleAutoGenerate(${record.id}, ${!record.auto_generate})"
                    class="${record.auto_generate ? 'text-green-600' : 'text-gray-400'} hover:opacity-70 transition-opacity">
                    ${record.auto_generate
                        ? '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 mx-auto" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" /></svg>'
                        : '<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5 mx-auto" viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" /></svg>'}
                </button>
            </td>
            <td class="px-6 py-4 text-center">
                <div class="flex justify-center gap-2">
                    <button onclick='showTokenConfigModal(${JSON.stringify(record).replace(/'/g, "\\'")})'
                        class="text-primary hover:text-primary-hover transition-colors" title="配置">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                        </svg>
                    </button>
                    ${record.is_active ? `
                    <button onclick="triggerGeneration(${record.id})"
                        class="text-blue-600 hover:text-blue-700 transition-colors" title="手动触发生成">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
                        </svg>
                    </button>` : ''}
                    ${record.has_refresh_token ? `
                    <button onclick="refreshToken(${record.id})"
                        class="text-green-600 hover:text-green-700 transition-colors" title="刷新令牌">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                        </svg>
                    </button>` : ''}
                    <button onclick="deleteTokenRecord(${record.id})"
                        class="text-red-500 hover:text-red-700 transition-colors" title="删除">
                        <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                        </svg>
                    </button>
                </div>
            </td>
        </tr>`;
    }).join('');
    
    tbody.innerHTML = html;
}

// 添加刷新令牌函数
async function refreshToken(tokenId) {
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}/refresh`, {
            method: 'POST',
            headers: getAuthHeaders()
        });
        
        if (!resp.ok) {
            const error = await resp.json();
            throw new Error(error.error || 'Failed to refresh token');
        }
        
        const result = await resp.json();
        showToast(result.message || 'Token刷新成功', 'success');
        loadTokenRecords();
    } catch (e) {
        showToast('Token刷新失败: ' + e.message, 'error');
    }
}

function renderGenerationTasks(tasks) {
    const tbody = document.getElementById('taskList');
    
    if (tasks.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="px-6 py-8 text-center text-sm text-gray-500 dark:text-gray-400">暂无生成任务记录</td></tr>';
        return;
    }
    
    const html = tasks.map(task => {
        const startTime = task.started_at && !task.started_at.startsWith('0001')
            ? new Date(task.started_at).toLocaleString('zh-CN')
            : '-';
        const completeTime = task.completed_at && !task.completed_at.startsWith('0001')
            ? new Date(task.completed_at).toLocaleString('zh-CN')
            : '-';
        
        let statusClass, statusText;
        switch (task.status) {
            case 'running':
                statusClass = 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400';
                statusText = '运行中';
                break;
            case 'completed':
                statusClass = 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400';
                statusText = '已完成';
                break;
            case 'failed':
                statusClass = 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400';
                statusText = '失败';
                break;
            default:
                statusClass = 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400';
                statusText = '待处理';
        }
        
        return `
        <tr class="hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors">
            <td class="px-6 py-3 text-sm text-gray-900 dark:text-white">#${task.id}</td>
            <td class="px-6 py-3 text-sm text-center text-gray-900 dark:text-white">${task.batch_size}</td>
            <td class="px-6 py-3 text-sm text-center">
                <span class="text-green-600 dark:text-green-400">${task.success_count}</span>
                <span class="text-gray-500">/</span>
                <span class="text-red-600 dark:text-red-400">${task.fail_count}</span>
            </td>
            <td class="px-6 py-3 text-center">
                <span class="px-2 py-0.5 inline-flex text-xs font-medium rounded-full ${statusClass}">
                    ${statusText}
                </span>
            </td>
            <td class="px-6 py-3 text-sm text-center text-gray-600 dark:text-gray-400">${startTime}</td>
            <td class="px-6 py-3 text-sm text-center text-gray-600 dark:text-gray-400">${completeTime}</td>
        </tr>`;
    }).join('');
    
    tbody.innerHTML = html;
}

async function quickToggleAutoGenerate(tokenId, enable) {
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify({ auto_generate: enable })
        });
        
        if (!resp.ok) throw new Error('Failed to update token');
        loadTokenRecords();
    } catch (e) {
        alert('更新失败: ' + e.message);
    }
}

async function quickToggleActive(tokenId, enable) {
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify({ is_active: enable })
        });
        
        if (!resp.ok) throw new Error('Failed to update token');
        loadTokenRecords();
    } catch (e) {
        alert('更新失败: ' + e.message);
    }
}

async function deleteTokenRecord(tokenId) {
    if (!confirm('确定要删除此令牌记录吗？')) return;
    
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}`, {
            method: 'DELETE',
            headers: getAuthHeaders()
        });
        
        if (!resp.ok) throw new Error('Failed to delete token');
        
        const result = await resp.json();
        alert(result.message || '删除成功');
        loadTokenRecords();
    } catch (e) {
        alert('删除失败: ' + e.message);
    }
}

async function triggerGeneration(tokenId) {
    if (!confirm('确定要手动触发生成任务吗？')) return;
    
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}/trigger`, {
            method: 'POST',
            headers: getAuthHeaders()
        });
        
        if (!resp.ok) throw new Error('Failed to trigger generation');
        
        alert('生成任务已触发，请稍后查看结果');
        setTimeout(() => {
            loadTokenData();
        }, 2000);
    } catch (e) {
        alert('触发失败: ' + e.message);
    }
}

let currentEditingToken = null;

function showTokenConfigModal(record) {
    currentEditingToken = record;
    document.getElementById('tokenConfigModal').classList.remove('hidden');
    
    // 填充表单数据
    document.getElementById('configTokenId').value = record.id;
    document.getElementById('configDescription').value = record.description || '';
    document.getElementById('configThreshold').value = record.threshold || 10;
    document.getElementById('configBatch').value = record.generate_batch || 30;
    
    // 设置开关状态
    setConfigSwitch('configAutoGenerate', record.auto_generate);
    setConfigSwitch('configIsActive', record.is_active);
}

function closeTokenConfigModal() {
    document.getElementById('tokenConfigModal').classList.add('hidden');
    currentEditingToken = null;
}

function setConfigSwitch(switchId, isOn) {
    const switchBtn = document.getElementById(switchId);
    const indicator = switchBtn.querySelector('span');
    
    if (isOn) {
        switchBtn.classList.remove('bg-gray-200', 'dark:bg-gray-600');
        switchBtn.classList.add('bg-primary');
        indicator.classList.remove('translate-x-1');
        indicator.classList.add('translate-x-6');
        switchBtn.dataset.checked = 'true';
    } else {
        switchBtn.classList.add('bg-gray-200', 'dark:bg-gray-600');
        switchBtn.classList.remove('bg-primary');
        indicator.classList.add('translate-x-1');
        indicator.classList.remove('translate-x-6');
        switchBtn.dataset.checked = 'false';
    }
}

function toggleConfigSwitch(switchId) {
    const switchBtn = document.getElementById(switchId);
    const isChecked = switchBtn.dataset.checked === 'true';
    setConfigSwitch(switchId, !isChecked);
}

// Token config form handler
document.getElementById('tokenConfigForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const tokenId = document.getElementById('configTokenId').value;
    const config = {
        description: document.getElementById('configDescription').value,
        threshold: parseInt(document.getElementById('configThreshold').value),
        generate_batch: parseInt(document.getElementById('configBatch').value),
        auto_generate: document.getElementById('configAutoGenerate').dataset.checked === 'true',
        is_active: document.getElementById('configIsActive').dataset.checked === 'true'
    };
    
    try {
        const resp = await fetch(`${API_BASE}/tokens/${tokenId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
                ...getAuthHeaders()
            },
            body: JSON.stringify(config)
        });
        
        if (!resp.ok) throw new Error('Failed to update token config');
        
        closeTokenConfigModal();
        loadTokenRecords();
    } catch (e) {
        alert('更新配置失败: ' + e.message);
    }
});

async function refreshTokenData() {
    await loadTokenData();
}

// OAuth RT获取功能
function startOAuthForRT() {
    // 打开新窗口进行OAuth认证
    const width = 600;
    const height = 700;
    const left = (window.screen.width - width) / 2;
    const top = (window.screen.height - height) / 2;
    
    const authWindow = window.open(
        '/api/oauth/start-rt',
        'ZenCoderOAuth',
        `width=${width},height=${height},left=${left},top=${top},toolbar=no,menubar=no,scrollbars=yes,resizable=yes`
    );
    
    // 监听OAuth完成消息
    window.addEventListener('message', function handleOAuthMessage(event) {
        // 验证消息来源
        if (event.origin !== window.location.origin) return;
        
        if (event.data.type === 'oauth-rt-complete') {
            // 关闭认证窗口
            if (authWindow && !authWindow.closed) {
                authWindow.close();
            }
            
            // 移除事件监听器
            window.removeEventListener('message', handleOAuthMessage);
            
            if (event.data.success && (event.data.accessToken || event.data.refreshToken)) {
                // 自动填充到对应的输入框
                const currentMode = currentAddMode;
                if (currentMode === 'credential') {
                    // 优先填充access_token
                    if (event.data.accessToken) {
                        document.getElementById('credential_access_token').value = event.data.accessToken;
                    }
                    // 同时填充refresh_token
                    if (event.data.refreshToken) {
                        document.getElementById('credential_refresh_token').value = event.data.refreshToken;
                    }
                    // 显示成功提示
                    showToast('Token 获取成功！', 'success');
                } else {
                    // 生成模式
                    if (event.data.accessToken) {
                        document.getElementById('generate_access_token').value = event.data.accessToken;
                    }
                    if (event.data.refreshToken) {
                        document.getElementById('generate_refresh_token').value = event.data.refreshToken;
                    }
                    showToast('Master Token 获取成功！', 'success');
                }
            } else {
                showToast(event.data.error || 'OAuth认证失败', 'error');
            }
        }
    });
}

// Toast提示功能
function showToast(message, type = 'info') {
    // 创建toast元素
    const toast = document.createElement('div');
    toast.className = `fixed top-4 right-4 px-6 py-3 rounded-lg shadow-lg transition-all duration-300 transform translate-x-96 z-50`;
    
    // 根据类型设置样式
    const styles = {
        success: 'bg-green-600 text-white',
        error: 'bg-red-600 text-white',
        warning: 'bg-yellow-500 text-white',
        info: 'bg-blue-600 text-white'
    };
    
    toast.className += ` ${styles[type] || styles.info}`;
    toast.innerHTML = `
        <div class="flex items-center gap-2">
            <span class="text-sm font-medium">${message}</span>
        </div>
    `;
    
    document.body.appendChild(toast);
    
    // 动画显示
    setTimeout(() => {
        toast.classList.remove('translate-x-96');
        toast.classList.add('translate-x-0');
    }, 10);
    
    // 3秒后自动消失
    setTimeout(() => {
        toast.classList.remove('translate-x-0');
        toast.classList.add('translate-x-96');
        setTimeout(() => {
            document.body.removeChild(toast);
        }, 300);
    }, 3000);
}

// --- Account Info Modal Management ---
function showAccountInfoModal(accountInfo) {
    document.getElementById('accountInfoModal').classList.remove('hidden');
    
    // 填充账号信息
    document.getElementById('accountInfoEmail').textContent = accountInfo.email || '未知';
    document.getElementById('accountInfoPlan').textContent = accountInfo.plan || 'Free';
    
    // 格式化Token过期时间
    let tokenExpiryText = '未知';
    if (accountInfo.token_expiry && !accountInfo.token_expiry.startsWith('0001')) {
        const expiryDate = new Date(accountInfo.token_expiry);
        const now = new Date();
        const diffMs = expiryDate - now;
        
        if (diffMs < 0) {
            tokenExpiryText = '已过期';
        } else if (diffMs < 1000 * 60 * 60 * 24) {
            // 24小时内过期
            const hours = Math.floor(diffMs / (1000 * 60 * 60));
            tokenExpiryText = `${hours}小时后过期`;
        } else {
            // 显示具体日期
            tokenExpiryText = expiryDate.toLocaleDateString('zh-CN') + ' ' + expiryDate.toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'});
        }
    }
    document.getElementById('accountInfoTokenExpiry').textContent = tokenExpiryText;
    
    // 格式化订阅开始时间
    let subscriptionStartText = '未知';
    if (accountInfo.subscription_start_date && !accountInfo.subscription_start_date.startsWith('0001')) {
        const startDate = new Date(accountInfo.subscription_start_date);
        subscriptionStartText = startDate.toLocaleDateString('zh-CN') + ' ' + startDate.toLocaleTimeString('zh-CN', {hour: '2-digit', minute: '2-digit'});
    }
    document.getElementById('accountInfoSubscriptionStart').textContent = subscriptionStartText;
}

function closeAccountInfoModal() {
    document.getElementById('accountInfoModal').classList.add('hidden');
    // 刷新账号列表
    loadAccounts();
}

// Page Initialization
window.addEventListener('load', function() {
    console.log('Page loaded, initializing...');
    initTheme();
    initAdminAuth();
});
