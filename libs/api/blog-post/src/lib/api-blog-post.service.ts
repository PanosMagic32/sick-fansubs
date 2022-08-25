import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model } from 'mongoose';

import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { UpdateBlogPostDto } from './dtos/update-blog-post.dto';
import { BlogPost, BlogPostDocument } from './schemas/blog-post.schema';

@Injectable()
export class ApiBlogPostService {
  constructor(@InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPostDocument>) {}

  async create(createBlogPostDto: CreateBlogPostDto): Promise<{ id: string }> {
    const createdBlogPost = await this.blogPostModel.create(createBlogPostDto);
    return { id: createdBlogPost._id };
  }

  async findAll(): Promise<BlogPost[]> {
    return this.blogPostModel.find().exec();
  }

  async findOne(id: string): Promise<BlogPost | undefined> {
    const blogPost = await this.blogPostModel.findOne({ _id: id });

    if (blogPost) {
      return blogPost;
    } else {
      throw new NotFoundException();
    }
  }

  async update(id: string, updateBlogPostDto: UpdateBlogPostDto): Promise<BlogPost | undefined | null> {
    const blogPost = await this.findOne(id);

    if (blogPost) {
      return this.blogPostModel.findByIdAndUpdate({ _id: id }, updateBlogPostDto).exec();
    } else {
      throw new NotFoundException();
    }
  }

  async delete(id: string) {
    const deletedBlogPost = await this.blogPostModel.findByIdAndRemove({ _id: id }).exec();
    return deletedBlogPost;
  }
}
