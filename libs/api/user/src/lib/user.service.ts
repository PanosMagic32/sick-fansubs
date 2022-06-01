import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model } from 'mongoose';

import { CreateUserDto } from './dto/create-user.dto';
import { UpdateUserDto } from './dto/update-user.dto';
import { User, UserDocument } from './schemas/user.schema';

@Injectable()
export class UserService {
  constructor(@InjectModel(User.name) private readonly userModel: Model<UserDocument>) {}

  async create(createUserDto: CreateUserDto): Promise<User> {
    const createdUser = await this.userModel.create(createUserDto);
    return createdUser;
  }

  async findAll(): Promise<User[]> {
    return this.userModel.find().exec();
  }

  async findOne(id: string): Promise<User | undefined> {
    const user = await this.userModel.findOne({ _id: id });

    if (user) {
      return user;
    } else {
      throw new NotFoundException();
    }
  }

  async update(id: string, updateUserDto: UpdateUserDto): Promise<User | undefined | null> {
    const user = await this.findOne(id);

    if (user) {
      return this.userModel.findByIdAndUpdate({ _id: id }, updateUserDto).exec();
    } else {
      throw new NotFoundException();
    }
  }

  async remove(id: string) {
    const deletedUser = await this.userModel.findByIdAndRemove({ _id: id }).exec();
    return deletedUser;
  }
}
