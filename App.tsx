import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
// ✅ FIXED: Changed import path from './components/layout/AppLayout' to './components/Layout'
import Layout from './components/Layout';
import ProductList from './pages/ProductList';
import ProductCreate from './pages/ProductCreate';
import ProductDetail from './pages/ProductDetail';
import CategoryList from './pages/CategoryList';
import VariantList from './pages/VariantList';
import AttributesPage from './pages/AttributesPage';

function App() {
  return (
    <BrowserRouter>
      <Layout>
        <Routes>
          {/* Default redirect to products */}
          <Route path="/" element={<Navigate to="/products" replace />} />
          
          {/* Product routes */}
          <Route path="/products" element={<ProductList />} />
          <Route path="/products/create" element={<ProductCreate />} />
          <Route path="/products/:id" element={<ProductDetail />} />
          
          {/* Category routes */}
          <Route path="/categories" element={<CategoryList />} />
          
          {/* Attributes routes */}
          <Route path="/attributes" element={<AttributesPage />} />
          
          {/* Variant routes */}
          <Route path="/variants" element={<VariantList />} />
          
          {/* Catch-all redirect */}
          <Route path="*" element={<Navigate to="/products" replace />} />
        </Routes>
      </Layout>
    </BrowserRouter>
  );
}

export default App;
