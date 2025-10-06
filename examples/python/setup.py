#!/usr/bin/env python3
"""
MinIODB Python SDK 安装脚本

此脚本用于安装 MinIODB Python SDK 及其依赖。
"""

from setuptools import setup, find_packages
import os

# 读取 README 文件
def read_readme():
    readme_path = os.path.join(os.path.dirname(__file__), 'README.md')
    if os.path.exists(readme_path):
        with open(readme_path, 'r', encoding='utf-8') as f:
            return f.read()
    return ''

# 读取版本信息
def read_version():
    version_path = os.path.join(os.path.dirname(__file__), 'miniodb_sdk', '__version__.py')
    if os.path.exists(version_path):
        with open(version_path, 'r', encoding='utf-8') as f:
            exec(f.read())
            return locals()['__version__']
    return '1.0.0'

setup(
    name='miniodb-sdk',
    version=read_version(),
    description='Python SDK for MinIODB - A distributed OLAP system based on MinIO, DuckDB and Redis',
    long_description=read_readme(),
    long_description_content_type='text/markdown',
    author='MinIODB Team',
    author_email='team@miniodb.com',
    url='https://github.com/your-org/minIODB',
    license='BSD-3-Clause',
    
    packages=find_packages(),
    include_package_data=True,
    
    python_requires='>=3.8',
    
    install_requires=[
        'grpcio>=1.58.0',
        'grpcio-tools>=1.58.0',
        'protobuf>=4.24.0',
        'requests>=2.31.0',
        'aiohttp>=3.8.0',
        'asyncio-throttle>=1.0.0',
        'tenacity>=8.2.0',
        'pydantic>=2.0.0',
        'python-dateutil>=2.8.0',
    ],
    
    extras_require={
        'dev': [
            'pytest>=7.4.0',
            'pytest-asyncio>=0.21.0',
            'pytest-cov>=4.1.0',
            'black>=23.0.0',
            'flake8>=6.0.0',
            'mypy>=1.5.0',
            'isort>=5.12.0',
        ],
        'docs': [
            'sphinx>=7.0.0',
            'sphinx-rtd-theme>=1.3.0',
            'sphinx-autodoc-typehints>=1.24.0',
        ],
    },
    
    classifiers=[
        'Development Status :: 4 - Beta',
        'Intended Audience :: Developers',
        'License :: OSI Approved :: BSD License',
        'Operating System :: OS Independent',
        'Programming Language :: Python :: 3',
        'Programming Language :: Python :: 3.8',
        'Programming Language :: Python :: 3.9',
        'Programming Language :: Python :: 3.10',
        'Programming Language :: Python :: 3.11',
        'Programming Language :: Python :: 3.12',
        'Topic :: Database',
        'Topic :: Software Development :: Libraries :: Python Modules',
    ],
    
    keywords='miniodb database olap grpc sdk client',
    
    entry_points={
        'console_scripts': [
            'miniodb-cli=miniodb_sdk.cli:main',
        ],
    },
    
    project_urls={
        'Bug Reports': 'https://github.com/your-org/minIODB/issues',
        'Source': 'https://github.com/your-org/minIODB',
        'Documentation': 'https://miniodb.readthedocs.io/',
    },
)
